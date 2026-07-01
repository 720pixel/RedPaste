package httpapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/720pRed/redpaste/internal/live"
	"github.com/720pRed/redpaste/internal/sqlite"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

func newLiveTestServer(t *testing.T) (*httptest.Server, *live.Service) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	svc := live.NewService(st, []byte("test-pepper-0123456789abcdef0123"), live.Limits{
		MaxParticipants: 4,
		MaxUpdateBytes:  1 << 16,
		MaxStorageBytes: 1 << 20,
		MaxRetention:    180 * 24 * time.Hour,
	})
	hub := live.NewHub(4, 100)
	h := NewLiveHandler(svc, hub, "http://x", "http://x", context.Background(), nil)

	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) { h.Mount(api) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, svc
}

func dialSignal(t *testing.T, ctx context.Context, srv *httptest.Server, slug string) *websocket.Conn {
	t.Helper()
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/api/v1/live/" + slug + "/signal"
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func sendJSON(t *testing.T, ctx context.Context, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readType reads frames until one of type want arrives, decoding it into out.
func readType(t *testing.T, ctx context.Context, c *websocket.Conn, want string, out any) {
	t.Helper()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read waiting for %q: %v", want, err)
		}
		var probe struct {
			T string `json:"t"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			continue
		}
		if probe.T == want {
			if out != nil {
				_ = json.Unmarshal(data, out)
			}
			return
		}
	}
}

func TestSignalRelaysUpdate(t *testing.T) {
	srv, svc := newLiveTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := svc.Create(ctx, live.CreateInput{
		EditorToken:      []byte("editor-token-bytes"),
		OwnerToken:       []byte("owner-token-bytes"),
		CryptoMetadata:   []byte(`{"v":1}`),
		ExpiresInSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	authTok := b64.EncodeToString([]byte("editor-token-bytes"))

	a := dialSignal(t, ctx, srv, res.Slug)
	defer func() { _ = a.CloseNow() }()
	sendJSON(t, ctx, a, map[string]string{"t": "auth", "token": authTok})

	b := dialSignal(t, ctx, srv, res.Slug)
	defer func() { _ = b.CloseNow() }()
	sendJSON(t, ctx, b, map[string]string{"t": "auth", "token": authTok})

	// Sending a's update races the server's own processing of b's auth frame —
	// a "write returned" is not "server has joined b to the room". Wait for
	// b's self-delivered join confirmation (sent right after hub.Join
	// succeeds) so the broadcast below is guaranteed to see b registered.
	readType(t, ctx, b, "peers", nil)

	// a sends an update; b should receive it with a seq assigned.
	env := b64.EncodeToString([]byte("encrypted-update"))
	sendJSON(t, ctx, a, map[string]string{"t": "update", "data": env})

	var got struct {
		T    string `json:"t"`
		Data string `json:"data"`
		Seq  int64  `json:"seq"`
	}
	readType(t, ctx, b, "update", &got)
	if got.Data != env {
		t.Errorf("relayed data = %q, want %q", got.Data, env)
	}
	if got.Seq != 1 {
		t.Errorf("seq = %d, want 1", got.Seq)
	}
}

func TestSignalRejectsBadAuth(t *testing.T) {
	srv, svc := newLiveTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := svc.Create(ctx, live.CreateInput{
		EditorToken:      []byte("editor-token-bytes"),
		OwnerToken:       []byte("owner-token-bytes"),
		CryptoMetadata:   []byte(`{"v":1}`),
		ExpiresInSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}

	c := dialSignal(t, ctx, srv, res.Slug)
	defer func() { _ = c.CloseNow() }()
	sendJSON(t, ctx, c, map[string]string{"t": "auth", "token": b64.EncodeToString([]byte("wrong"))})

	// Server closes on bad auth, so the next read must fail.
	if _, _, err := c.Read(ctx); err == nil {
		t.Error("expected connection to be closed after bad auth")
	}
}
