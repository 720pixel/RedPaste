// Package live is the business logic for real-time collaboration rooms: create,
// bootstrap, relay+persist encrypted updates, snapshot, freeze, delete. Like the
// paste service it sits between the transport layer and the store and owns policy
// (participant, size, and storage caps, retention). The server is zero-knowledge:
// it only ever handles opaque encrypted blobs and capability hashes.
package live

import (
	"context"
	"errors"
	"time"

	"github.com/720pRed/redpaste/internal/cryptoformat"
	"github.com/720pRed/redpaste/internal/store"
)

// shortest allowed lifetime; stops someone opening a room that's already dead.
const minRetention = 60 * time.Second

// Snapshots compact the whole room, so they get a bigger ceiling than a single update.
const maxSnapshotBytes = 2 << 20

var (
	ErrTooLarge     = errors.New("ciphertext too large")
	ErrBadRetention = errors.New("retention out of range")
	ErrBadMetadata  = errors.New("bad crypto metadata")
	ErrBadToken     = errors.New("bad capability token")
	ErrUnavailable  = errors.New("room unavailable")
	ErrForbidden    = errors.New("forbidden")
	ErrFrozen       = errors.New("room frozen")
	ErrStorageFull  = errors.New("room storage full")
)

// Limits are the policy knobs the service enforces, pulled from config.
type Limits struct {
	MaxParticipants   int
	MaxUpdateBytes    int64
	MaxStorageBytes   int64
	MaxRetention      time.Duration
	ShortLinksEnabled bool
}

type Service struct {
	store  store.Store
	pepper []byte
	limits Limits
}

func NewService(s store.Store, pepper []byte, limits Limits) *Service {
	return &Service{store: s, pepper: pepper, limits: limits}
}

// MaxParticipants exposes the configured cap for the transport layer.
func (s *Service) MaxParticipants() int { return s.limits.MaxParticipants }

// MaxUpdateBytes exposes the per-update size cap for the transport layer.
func (s *Service) MaxUpdateBytes() int64 { return s.limits.MaxUpdateBytes }

type CreateInput struct {
	EditorToken      []byte // raw T_editor; hashed with the pepper, never stored raw
	OwnerToken       []byte // raw ownerToken; hashed with the pepper
	CryptoMetadata   []byte
	InitialSnapshot  []byte // optional seed snapshot envelope
	ExpiresInSeconds int64
	ShortLink        bool
}

type CreateResult struct {
	Slug      string
	ShortCode string // "" when no short link
	ExpiresAt time.Time
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	if len(in.EditorToken) == 0 || len(in.OwnerToken) == 0 {
		return nil, ErrBadToken
	}
	if _, err := cryptoformat.ValidateMetadata(in.CryptoMetadata); err != nil {
		return nil, ErrBadMetadata
	}
	if int64(len(in.InitialSnapshot)) > maxSnapshotBytes {
		return nil, ErrTooLarge
	}

	d := time.Duration(in.ExpiresInSeconds) * time.Second
	if d < minRetention || d > s.limits.MaxRetention {
		return nil, ErrBadRetention
	}

	editorHash := cryptoformat.HashCapability(s.pepper, in.EditorToken)
	ownerHash := cryptoformat.HashCapability(s.pepper, in.OwnerToken)
	expiresAt := time.Now().UTC().Add(d)

	// Retry on the rare slug/short-code collision.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		params := store.CreateRoomParams{
			Slug:            cryptoformat.NewSlug(),
			EditorTokenHash: editorHash,
			OwnerTokenHash:  ownerHash,
			CryptoMetadata:  in.CryptoMetadata,
			ExpiresAt:       expiresAt,
			InitialSnapshot: in.InitialSnapshot,
		}
		var code string
		if in.ShortLink && s.limits.ShortLinksEnabled {
			code = cryptoformat.NewShortCode()
			params.ShortCode = &code
		}
		r, err := s.store.CreateRoom(ctx, params)
		if err == nil {
			return &CreateResult{Slug: r.Slug, ShortCode: code, ExpiresAt: r.ExpiresAt}, nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// Item is one opaque encrypted blob with its sequence number.
type Item struct {
	Seq        int64
	Ciphertext []byte
}

type BootstrapResult struct {
	State           string // "active" | "frozen"
	Snapshot        *Item
	Updates         []Item
	ExpiresAt       time.Time
	MaxParticipants int
	CryptoMetadata  []byte // opaque; lets a joiner see if a password is needed
}

// Bootstrap authenticates an editor and returns everything needed to rebuild the
// current document: the latest snapshot plus every update after it.
func (s *Service) Bootstrap(ctx context.Context, slug string, editorToken []byte) (*BootstrapResult, error) {
	if len(editorToken) == 0 {
		return nil, ErrForbidden
	}
	hash := cryptoformat.HashCapability(s.pepper, editorToken)

	room, err := s.store.GetRoomForEditor(ctx, slug, hash)
	if err != nil {
		return nil, mapErr(err)
	}
	snap, updates, err := s.store.BootstrapRoom(ctx, room)
	if err != nil {
		return nil, mapErr(err)
	}

	res := &BootstrapResult{
		State:           room.State,
		ExpiresAt:       room.ExpiresAt,
		MaxParticipants: s.limits.MaxParticipants,
		CryptoMetadata:  room.CryptoMetadata,
	}
	if snap != nil {
		res.Snapshot = &Item{Seq: snap.Seq, Ciphertext: snap.Ciphertext}
	}
	for _, u := range updates {
		res.Updates = append(res.Updates, Item{Seq: u.Seq, Ciphertext: u.Ciphertext})
	}
	return res, nil
}

// VerifyEditor checks an editor token for the WebSocket handshake and reports the
// current room state so the caller knows whether writes are allowed.
func (s *Service) VerifyEditor(ctx context.Context, slug string, editorToken []byte) (state string, err error) {
	if len(editorToken) == 0 {
		return "", ErrForbidden
	}
	hash := cryptoformat.HashCapability(s.pepper, editorToken)
	room, err := s.store.GetRoomForEditor(ctx, slug, hash)
	if err != nil {
		return "", mapErr(err)
	}
	return room.State, nil
}

// AppendUpdate persists a relayed update. Auth already happened at the WS
// handshake, so this only enforces size and storage caps.
func (s *Service) AppendUpdate(ctx context.Context, slug string, ciphertext []byte) (int64, error) {
	if int64(len(ciphertext)) > s.limits.MaxUpdateBytes || len(ciphertext) == 0 {
		return 0, ErrTooLarge
	}
	seq, err := s.store.AppendUpdate(ctx, slug, ciphertext, s.limits.MaxStorageBytes)
	if err != nil {
		return 0, mapErr(err)
	}
	return seq, nil
}

// SaveSnapshot compacts the room: it replaces the latest snapshot and drops every
// update at or below baseSeq.
func (s *Service) SaveSnapshot(ctx context.Context, slug string, editorToken, ciphertext []byte, baseSeq int64) error {
	if len(editorToken) == 0 {
		return ErrForbidden
	}
	if int64(len(ciphertext)) > maxSnapshotBytes || len(ciphertext) == 0 {
		return ErrTooLarge
	}
	if int64(len(ciphertext)) > s.limits.MaxStorageBytes {
		return ErrStorageFull
	}
	hash := cryptoformat.HashCapability(s.pepper, editorToken)
	if _, err := s.store.GetRoomForEditor(ctx, slug, hash); err != nil {
		return mapErr(err)
	}
	if err := s.store.SaveSnapshot(ctx, slug, ciphertext, baseSeq); err != nil {
		return mapErr(err)
	}
	return nil
}

func (s *Service) Freeze(ctx context.Context, slug string, ownerToken []byte) error {
	if len(ownerToken) == 0 {
		return ErrForbidden
	}
	hash := cryptoformat.HashCapability(s.pepper, ownerToken)
	if err := s.store.FreezeRoom(ctx, slug, hash); err != nil {
		// Wrong token and missing room look the same on purpose.
		return ErrForbidden
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, slug string, ownerToken []byte) error {
	if len(ownerToken) == 0 {
		return ErrForbidden
	}
	hash := cryptoformat.HashCapability(s.pepper, ownerToken)
	if err := s.store.DeleteRoom(ctx, slug, hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Idempotent + non-leaky: nothing to delete reads as success.
			return nil
		}
		return err
	}
	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ErrUnavailable
	case errors.Is(err, store.ErrFrozen):
		return ErrFrozen
	case errors.Is(err, store.ErrStorageFull):
		return ErrStorageFull
	default:
		return err
	}
}
