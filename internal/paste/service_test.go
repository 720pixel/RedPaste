package paste

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/720pRed/redpaste/internal/sqlite"
)

func newSvc(t *testing.T) *Service {
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
	return NewService(st, []byte("test-pepper-0123456789abcdef0123"), Limits{
		MaxCiphertextBytes: 4718592, // 4.5 MiB, matches the production default (3 MiB * 1.5)
		MaxRetention:       180 * 24 * time.Hour,
		MaxViews:           1000,
		ShortLinksEnabled:  true,
	})
}

func goodInput() CreateInput {
	return CreateInput{
		Ciphertext:       []byte("ciphertext"),
		CryptoMetadata:   []byte(`{"v":1,"alg":"AES-256-GCM"}`),
		ExpiresInSeconds: 3600,
		OwnerToken:       []byte("owner-token-bytes"),
		ShortLink:        true,
	}
}

func TestCreateAndRedeem(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	res, err := s.Create(ctx, goodInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.ShortCode == "" {
		t.Error("expected a short code")
	}
	rev, err := s.Redeem(ctx, res.Slug)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if string(rev.Ciphertext) != "ciphertext" {
		t.Errorf("got %q", rev.Ciphertext)
	}
}

func TestCreateRejectsBadRetention(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)

	in := goodInput()
	in.ExpiresInSeconds = 5 // below min
	if _, err := s.Create(ctx, in); err != ErrBadRetention {
		t.Errorf("too short: err = %v, want ErrBadRetention", err)
	}

	in = goodInput()
	in.ExpiresInSeconds = int64((181 * 24 * time.Hour).Seconds()) // over 180 days
	if _, err := s.Create(ctx, in); err != ErrBadRetention {
		t.Errorf("too long: err = %v, want ErrBadRetention", err)
	}
}

func TestCreateRejectsBadMetadataAndSize(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)

	in := goodInput()
	in.CryptoMetadata = []byte(`{"v":2}`) // unsupported version
	if _, err := s.Create(ctx, in); err != ErrBadMetadata {
		t.Errorf("bad version: err = %v, want ErrBadMetadata", err)
	}

	in = goodInput()
	in.Ciphertext = nil
	if _, err := s.Create(ctx, in); err != ErrTooLarge {
		t.Errorf("empty ciphertext: err = %v, want ErrTooLarge", err)
	}
}

func TestCreateAcceptsThreeMegabyteCiphertext(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)

	in := goodInput()
	in.Ciphertext = make([]byte, 4718592) // exactly the configured limit
	if _, err := s.Create(ctx, in); err != nil {
		t.Errorf("at limit: err = %v, want nil", err)
	}

	in = goodInput()
	in.Ciphertext = make([]byte, 4718592+1) // one byte over
	if _, err := s.Create(ctx, in); err != ErrTooLarge {
		t.Errorf("over limit: err = %v, want ErrTooLarge", err)
	}
}

func TestOwnerAuth(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	res, err := s.Create(ctx, goodInput())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Status(ctx, res.Slug, []byte("wrong-token")); err != ErrForbidden {
		t.Errorf("wrong token status: err = %v, want ErrForbidden", err)
	}
	if _, err := s.Status(ctx, res.Slug, []byte("owner-token-bytes")); err != nil {
		t.Errorf("right token status: %v", err)
	}

	// delete with the right token, then the paste is gone
	if err := s.Delete(ctx, res.Slug, []byte("owner-token-bytes")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Meta(ctx, res.Slug); err != ErrUnavailable {
		t.Errorf("meta after delete: err = %v, want ErrUnavailable", err)
	}
}
