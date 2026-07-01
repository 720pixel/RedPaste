package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/720pRed/redpaste/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mkParams(slug string) store.CreatePasteParams {
	return store.CreatePasteParams{
		Slug:           slug,
		Ciphertext:     []byte("ciphertext-bytes"),
		CryptoMetadata: []byte(`{"version":1}`),
		ExpiresAt:      time.Now().Add(time.Hour),
		OwnerTokenHash: []byte("owner-hash"),
	}
}

func TestCreateAndRedeem(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.CreatePaste(ctx, mkParams("slug1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	meta, err := s.GetPasteMeta(ctx, "slug1")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if meta.RequiresExplicitReveal {
		t.Error("normal paste should not require explicit reveal")
	}
	p, err := s.RedeemPaste(ctx, "slug1")
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if string(p.Ciphertext) != "ciphertext-bytes" {
		t.Errorf("wrong ciphertext: %q", p.Ciphertext)
	}
}

func TestBurnAfterReadConsumesOnFirstRead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	params := mkParams("burn")
	params.BurnAfterRead = true // no MaxViews set: burn must still be one-shot
	if _, err := s.CreatePaste(ctx, params); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.RedeemPaste(ctx, "burn"); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := s.RedeemPaste(ctx, "burn"); err != store.ErrNotFound && err != store.ErrExhausted {
		t.Errorf("second redeem err = %v, want unavailable (burned)", err)
	}
	if _, err := s.GetPasteMeta(ctx, "burn"); err != store.ErrNotFound {
		t.Errorf("meta after burn err = %v, want ErrNotFound", err)
	}
}

func TestMaxViewsExhaustion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	params := mkParams("limited")
	two := 2
	params.MaxViews = &two
	if _, err := s.CreatePaste(ctx, params); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.RedeemPaste(ctx, "limited"); err != nil {
			t.Fatalf("redeem %d: %v", i, err)
		}
	}
	// Once the last view is gone the paste is exhausted and reports the generic
	// unavailable error; we don't distinguish exhausted from never-existed.
	if _, err := s.RedeemPaste(ctx, "limited"); err != store.ErrNotFound && err != store.ErrExhausted {
		t.Errorf("3rd redeem err = %v, want unavailable (NotFound/Exhausted)", err)
	}
}

func TestConcurrentRedemptionRespectsLimit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	params := mkParams("race")
	one := 1
	params.MaxViews = &one
	if _, err := s.CreatePaste(ctx, params); err != nil {
		t.Fatalf("create: %v", err)
	}
	const n = 20
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := s.RedeemPaste(ctx, "race")
			results <- err
		}()
	}
	success := 0
	for i := 0; i < n; i++ {
		if err := <-results; err == nil {
			success++
		}
	}
	if success != 1 {
		t.Errorf("successful redemptions = %d, want exactly 1", success)
	}
}

func TestRevokeMakesUnavailable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.CreatePaste(ctx, mkParams("rev")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.RevokePaste(ctx, "rev", []byte("owner-hash")); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.GetPasteMeta(ctx, "rev"); err != store.ErrNotFound {
		t.Errorf("meta after revoke err = %v, want ErrNotFound", err)
	}
}

func TestExpiredNotAvailable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	params := mkParams("exp")
	params.ExpiresAt = time.Now().Add(50 * time.Millisecond)
	if _, err := s.CreatePaste(ctx, params); err != nil {
		t.Fatalf("create: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := s.GetPasteMeta(ctx, "exp"); err != store.ErrNotFound {
		t.Errorf("meta after expiry err = %v, want ErrNotFound", err)
	}
	n, err := s.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
}
