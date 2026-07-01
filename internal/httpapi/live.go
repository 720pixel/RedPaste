package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/720pRed/redpaste/internal/cryptoformat"
	"github.com/720pRed/redpaste/internal/live"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// Keepalive and safety timings for a live connection.
const (
	wsAuthTimeout   = 10 * time.Second
	wsWriteTimeout  = 10 * time.Second
	wsPingInterval  = 30 * time.Second
	wsMaxMsgsPerSec = 30
)

// LiveHandler is the browser-facing REST + WebSocket layer over the live service.
// Everything in and out is ciphertext or opaque metadata: no plaintext, ever.
type LiveHandler struct {
	svc         *live.Service
	hub         *live.Hub
	baseURL     string
	shortBase   string
	shutdownCtx context.Context
	logger      *slog.Logger
}

func NewLiveHandler(svc *live.Service, hub *live.Hub, baseURL, shortBase string, shutdownCtx context.Context, logger *slog.Logger) *LiveHandler {
	return &LiveHandler{
		svc:         svc,
		hub:         hub,
		baseURL:     strings.TrimRight(baseURL, "/"),
		shortBase:   strings.TrimRight(shortBase, "/"),
		shutdownCtx: shutdownCtx,
		logger:      logger,
	}
}

// Mount hangs the live routes off the given router (expected at /api/v1).
func (h *LiveHandler) Mount(r chi.Router) {
	r.Post("/live", h.create)
	r.Get("/live/{slug}/bootstrap", h.bootstrap)
	r.Post("/live/{slug}/snapshot", h.snapshot)
	r.Post("/live/{slug}/freeze", h.freeze)
	r.Delete("/live/{slug}", h.delete)
	r.Get("/live/{slug}/signal", h.signal)
}

// --- REST ---

type liveCreateRequest struct {
	EditorToken      string          `json:"editorToken"`
	OwnerToken       string          `json:"ownerToken"`
	CryptoMetadata   json.RawMessage `json:"cryptoMetadata"`
	Snapshot         *string         `json:"snapshot"`
	ExpiresInSeconds int64           `json:"expiresInSeconds"`
	ShortLink        bool            `json:"shortLink"`
}

type liveCreateResponse struct {
	Slug         string    `json:"slug"`
	URLBase      string    `json:"urlBase"`
	ShortURLBase *string   `json:"shortUrlBase"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func (h *LiveHandler) create(w http.ResponseWriter, r *http.Request) {
	if !wantsJSON(r) {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	var req liveCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	editorTok, err := b64.DecodeString(req.EditorToken)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_editor_token")
		return
	}
	ownerTok, err := b64.DecodeString(req.OwnerToken)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_owner_token")
		return
	}
	var snapshot []byte
	if req.Snapshot != nil && *req.Snapshot != "" {
		snapshot, err = b64.DecodeString(*req.Snapshot)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_snapshot")
			return
		}
	}

	res, err := h.svc.Create(r.Context(), live.CreateInput{
		EditorToken:      editorTok,
		OwnerToken:       ownerTok,
		CryptoMetadata:   req.CryptoMetadata,
		InitialSnapshot:  snapshot,
		ExpiresInSeconds: req.ExpiresInSeconds,
		ShortLink:        req.ShortLink,
	})
	if err != nil {
		writeLiveCreateErr(w, err)
		return
	}

	resp := liveCreateResponse{
		Slug:      res.Slug,
		URLBase:   h.baseURL + "/live/" + res.Slug,
		ExpiresAt: res.ExpiresAt,
	}
	if res.ShortCode != "" {
		s := h.shortBase + "/s/" + res.ShortCode
		resp.ShortURLBase = &s
	}
	writeJSON(w, http.StatusCreated, resp)
}

type liveItem struct {
	Seq        int64  `json:"seq"`
	Ciphertext string `json:"ciphertext"`
}

type liveBootstrapResponse struct {
	State           string          `json:"state"`
	Snapshot        *liveItem       `json:"snapshot"`
	Updates         []liveItem      `json:"updates"`
	ExpiresAt       time.Time       `json:"expiresAt"`
	MaxParticipants int             `json:"maxParticipants"`
	Crypto          json.RawMessage `json:"crypto,omitempty"`
}

func (h *LiveHandler) bootstrap(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	res, err := h.svc.Bootstrap(r.Context(), chi.URLParam(r, "slug"), tok)
	if err != nil {
		writeLiveUnavailable(w, err)
		return
	}
	resp := liveBootstrapResponse{
		State:           res.State,
		Updates:         make([]liveItem, 0, len(res.Updates)),
		ExpiresAt:       res.ExpiresAt,
		MaxParticipants: res.MaxParticipants,
		Crypto:          json.RawMessage(res.CryptoMetadata),
	}
	if res.Snapshot != nil {
		resp.Snapshot = &liveItem{Seq: res.Snapshot.Seq, Ciphertext: b64.EncodeToString(res.Snapshot.Ciphertext)}
	}
	for _, u := range res.Updates {
		resp.Updates = append(resp.Updates, liveItem{Seq: u.Seq, Ciphertext: b64.EncodeToString(u.Ciphertext)})
	}
	writeJSON(w, http.StatusOK, resp)
}

type liveSnapshotRequest struct {
	Ciphertext string `json:"ciphertext"`
	BaseSeq    int64  `json:"baseSeq"`
}

func (h *LiveHandler) snapshot(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req liveSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	ct, err := b64.DecodeString(req.Ciphertext)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_ciphertext")
		return
	}
	if err := h.svc.SaveSnapshot(r.Context(), chi.URLParam(r, "slug"), tok, ct, req.BaseSeq); err != nil {
		switch {
		case errors.Is(err, live.ErrTooLarge):
			writeErr(w, http.StatusRequestEntityTooLarge, "too_large")
		case errors.Is(err, live.ErrStorageFull):
			writeErr(w, http.StatusInsufficientStorage, "storage_full")
		default:
			writeLiveUnavailable(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *LiveHandler) freeze(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.svc.Freeze(r.Context(), chi.URLParam(r, "slug"), tok); err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "frozen"})
}

func (h *LiveHandler) delete(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "slug"), tok); err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- WebSocket relay ---

type wsIn struct {
	T     string `json:"t"`
	Token string `json:"token"`
	Data  string `json:"data"`
}

func (h *LiveHandler) signal(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return // Accept already wrote an error response
	}
	defer func() { _ = conn.CloseNow() }()

	// Cap the frame size: an update envelope of MaxUpdateBytes becomes ~4/3 in
	// base64 and rides inside a small JSON wrapper, so give some headroom.
	conn.SetReadLimit(h.svc.MaxUpdateBytes()*2 + 4096)

	// connCtx dies on server shutdown, request end, or when either pump exits.
	connCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-h.shutdownCtx.Done():
		case <-r.Context().Done():
		case <-connCtx.Done():
		}
		cancel()
	}()

	// First message must authenticate. No token ever rides in the query string.
	authCtx, authCancel := context.WithTimeout(connCtx, wsAuthTimeout)
	typ, raw, err := conn.Read(authCtx)
	authCancel()
	if err != nil || typ != websocket.MessageText {
		_ = conn.Close(websocket.StatusPolicyViolation, "auth required")
		return
	}
	var auth wsIn
	if err := json.Unmarshal(raw, &auth); err != nil || auth.T != "auth" {
		_ = conn.Close(websocket.StatusPolicyViolation, "auth required")
		return
	}
	token, ok := cryptoformat.DecodeToken(auth.Token)
	if !ok {
		_ = conn.Close(websocket.StatusPolicyViolation, "bad token")
		return
	}
	if _, err := h.svc.VerifyEditor(connCtx, slug, token); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	// Enforce participant + per-IP caps at join.
	peer, err := h.hub.Join(slug, remoteIP(r))
	if err != nil {
		if errors.Is(err, live.ErrRoomFull) {
			// Best-effort notice before closing; no write pump running yet.
			wctx, c := context.WithTimeout(connCtx, wsWriteTimeout)
			_ = conn.Write(wctx, websocket.MessageText, mustJSON(map[string]string{"t": "error", "code": "room_full"}))
			c()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "cannot join")
		return
	}
	defer func() {
		h.hub.Leave(peer)
		h.broadcastPeers(slug)
	}()

	// One writer goroutine owns the connection from here on.
	go h.writePump(connCtx, cancel, conn, peer)

	// Announce presence to everyone (self included).
	peer.Deliver(mustJSON(map[string]any{"t": "peers", "count": h.hub.Count(slug)}))
	h.broadcastPeers(slug)

	rl := &rateLimiter{max: wsMaxMsgsPerSec}
	for {
		typ, raw, err := conn.Read(connCtx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		if !rl.allow() {
			continue // silently drop past the rate cap
		}
		if !h.handleMessage(connCtx, peer, slug, raw) {
			return
		}
	}
}

// handleMessage processes one client frame; returns false when the connection
// should be torn down (room gone).
func (h *LiveHandler) handleMessage(ctx context.Context, peer *live.Peer, slug string, raw []byte) bool {
	var m wsIn
	if err := json.Unmarshal(raw, &m); err != nil {
		return true
	}
	switch m.T {
	case "update":
		env, err := b64.DecodeString(m.Data)
		if err != nil || len(env) == 0 {
			return true
		}
		seq, err := h.svc.AppendUpdate(ctx, slug, env)
		if err != nil {
			switch {
			case errors.Is(err, live.ErrFrozen):
				peer.Deliver(mustJSON(map[string]string{"t": "error", "code": "frozen"}))
			case errors.Is(err, live.ErrStorageFull):
				peer.Deliver(mustJSON(map[string]string{"t": "error", "code": "storage_full"}))
			case errors.Is(err, live.ErrTooLarge):
				peer.Deliver(mustJSON(map[string]string{"t": "error", "code": "too_large"}))
			case errors.Is(err, live.ErrUnavailable):
				return false // room revoked/expired mid-session
			}
			return true
		}
		h.hub.Broadcast(slug, peer, mustJSON(map[string]any{"t": "update", "data": m.Data, "seq": seq}))
	case "awareness":
		// Ephemeral presence/cursor state; relayed, never persisted.
		if m.Data == "" {
			return true
		}
		h.hub.Broadcast(slug, peer, mustJSON(map[string]any{"t": "awareness", "data": m.Data}))
	}
	return true
}

// writePump is the sole writer for a connection: it drains the peer's outbound
// queue and sends keepalive pings. It cancels the connection on any failure.
func (h *LiveHandler) writePump(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, peer *live.Peer) {
	defer cancel()
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.Dead():
			_ = conn.Close(websocket.StatusPolicyViolation, "slow consumer")
			return
		case msg := <-peer.Send():
			wctx, c := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			c()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, c := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pctx)
			c()
			if err != nil {
				return
			}
		}
	}
}

func (h *LiveHandler) broadcastPeers(slug string) {
	h.hub.Broadcast(slug, nil, mustJSON(map[string]any{"t": "peers", "count": h.hub.Count(slug)}))
}

// rateLimiter is a per-connection fixed-window counter, cheap and good enough
// to cap message floods without any background goroutine.
type rateLimiter struct {
	max         int
	windowStart time.Time
	count       int
}

func (rl *rateLimiter) allow() bool {
	now := time.Now()
	if now.Sub(rl.windowStart) >= time.Second {
		rl.windowStart = now
		rl.count = 0
	}
	rl.count++
	return rl.count <= rl.max
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeLiveCreateErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, live.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large")
	case errors.Is(err, live.ErrBadRetention):
		writeErr(w, http.StatusBadRequest, "bad_retention")
	case errors.Is(err, live.ErrBadMetadata):
		writeErr(w, http.StatusBadRequest, "bad_metadata")
	case errors.Is(err, live.ErrBadToken):
		writeErr(w, http.StatusBadRequest, "bad_token")
	default:
		writeErr(w, http.StatusInternalServerError, "internal_error")
	}
}

// Expired/revoked/missing rooms all collapse to a generic 410 so we never reveal which.
func writeLiveUnavailable(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, live.ErrUnavailable):
		writeErr(w, http.StatusGone, "unavailable")
	case errors.Is(err, live.ErrForbidden):
		writeErr(w, http.StatusForbidden, "forbidden")
	default:
		writeErr(w, http.StatusInternalServerError, "internal_error")
	}
}
