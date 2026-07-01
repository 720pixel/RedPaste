package live

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/720pRed/redpaste/internal/cryptoformat"
	"github.com/720pRed/redpaste/internal/sqlite"
	"github.com/720pRed/redpaste/internal/store"
)

func newSvc(t *testing.T) (*Service, store.Store) {
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
	svc := NewService(st, []byte("test-pepper-0123456789abcdef0123"), Limits{
		MaxParticipants:   8,
		MaxUpdateBytes:    1 << 16,
		MaxStorageBytes:   1 << 20,
		MaxRetention:      180 * 24 * time.Hour,
		ShortLinksEnabled: true,
	})
	return svc, st
}

func goodCreate() CreateInput {
	return CreateInput{
		EditorToken:      []byte("editor-token-bytes"),
		OwnerToken:       []byte("owner-token-bytes"),
		CryptoMetadata:   []byte(`{"v":1,"alg":"AES-256-GCM"}`),
		ExpiresInSeconds: 3600,
		ShortLink:        true,
	}
}

func TestCreateAndBootstrap(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc(t)
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.ShortCode == "" {
		t.Error("expected a short code")
	}

	boot, err := s.Bootstrap(ctx, res.Slug, []byte("editor-token-bytes"))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if boot.State != "active" {
		t.Errorf("state = %q, want active", boot.State)
	}
	if boot.MaxParticipants != 8 {
		t.Errorf("maxParticipants = %d, want 8", boot.MaxParticipants)
	}
	if len(boot.Updates) != 0 {
		t.Errorf("expected no updates, got %d", len(boot.Updates))
	}
}

func TestBootstrapRejectsWrongEditor(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc(t)
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Bootstrap(ctx, res.Slug, []byte("wrong-editor")); err != ErrUnavailable {
		t.Errorf("wrong editor: err = %v, want ErrUnavailable", err)
	}
}

func TestFreezeAndDeleteAuth(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc(t)
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatal(err)
	}

	// Wrong owner token is forbidden.
	if err := s.Freeze(ctx, res.Slug, []byte("nope")); err != ErrForbidden {
		t.Errorf("wrong owner freeze: err = %v, want ErrForbidden", err)
	}

	if err := s.Freeze(ctx, res.Slug, []byte("owner-token-bytes")); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	// Editor can still read a frozen room, and it reports frozen.
	boot, err := s.Bootstrap(ctx, res.Slug, []byte("editor-token-bytes"))
	if err != nil {
		t.Fatalf("bootstrap frozen: %v", err)
	}
	if boot.State != "frozen" {
		t.Errorf("state = %q, want frozen", boot.State)
	}

	// Delete with the right owner token, then the room is gone.
	if err := s.Delete(ctx, res.Slug, []byte("owner-token-bytes")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Bootstrap(ctx, res.Slug, []byte("editor-token-bytes")); err != ErrUnavailable {
		t.Errorf("bootstrap after delete: err = %v, want ErrUnavailable", err)
	}
	// Delete is idempotent + non-leaky.
	if err := s.Delete(ctx, res.Slug, []byte("owner-token-bytes")); err != nil {
		t.Errorf("second delete: %v", err)
	}
}

func TestAppendThenSnapshotPrunes(t *testing.T) {
	ctx := context.Background()
	s, st := newSvc(t)
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := s.AppendUpdate(ctx, res.Slug, []byte("update-envelope")); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	boot, err := s.Bootstrap(ctx, res.Slug, []byte("editor-token-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(boot.Updates) != 3 {
		t.Fatalf("expected 3 updates, got %d", len(boot.Updates))
	}

	// Snapshot at base_seq 2 folds in the first two updates.
	if err := s.SaveSnapshot(ctx, res.Slug, []byte("editor-token-bytes"), []byte("snapshot-envelope"), 2); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	boot, err = s.Bootstrap(ctx, res.Slug, []byte("editor-token-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if boot.Snapshot == nil || boot.Snapshot.Seq != 2 {
		t.Fatalf("expected snapshot at seq 2, got %+v", boot.Snapshot)
	}
	if len(boot.Updates) != 1 || boot.Updates[0].Seq != 3 {
		t.Fatalf("expected only update seq 3 after prune, got %+v", boot.Updates)
	}

	// Storage should have been recomputed to snapshot + one remaining update.
	room, err := st.GetRoomForOwner(ctx, res.Slug, hashOwner(s))
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len("snapshot-envelope") + len("update-envelope"))
	if room.StorageBytes != want {
		t.Errorf("storage_bytes = %d, want %d", room.StorageBytes, want)
	}
}

func TestFreezeRejectsUpdates(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc(t)
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Freeze(ctx, res.Slug, []byte("owner-token-bytes")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendUpdate(ctx, res.Slug, []byte("late-update")); err != ErrFrozen {
		t.Errorf("append after freeze: err = %v, want ErrFrozen", err)
	}
}

func TestStorageCap(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc(t)
	// Tighten the storage cap so a couple of updates blow past it.
	s.limits.MaxStorageBytes = 20
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendUpdate(ctx, res.Slug, []byte("0123456789")); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if _, err := s.AppendUpdate(ctx, res.Slug, []byte("0123456789")); err != nil {
		t.Fatalf("second append: %v", err)
	}
	// Third would exceed 20 bytes.
	if _, err := s.AppendUpdate(ctx, res.Slug, []byte("0123456789")); err != ErrStorageFull {
		t.Errorf("third append: err = %v, want ErrStorageFull", err)
	}
}

func TestExpiredPurge(t *testing.T) {
	ctx := context.Background()
	s, st := newSvc(t)
	res, err := s.Create(ctx, goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendUpdate(ctx, res.Slug, []byte("update")); err != nil {
		t.Fatal(err)
	}

	// Sweep with a far-future clock so the room and its updates get purged.
	n, err := st.DeleteExpired(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n == 0 {
		t.Error("expected at least one row purged")
	}
	if _, err := s.Bootstrap(ctx, res.Slug, []byte("editor-token-bytes")); err != ErrUnavailable {
		t.Errorf("bootstrap after purge: err = %v, want ErrUnavailable", err)
	}
}

// hashOwner mirrors how the service hashes the owner token so tests can query
// the store directly.
func hashOwner(s *Service) []byte {
	return cryptoformat.HashCapability(s.pepper, []byte("owner-token-bytes"))
}
