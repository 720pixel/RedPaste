// Package store is the persistence contract and its domain types. The server is
// zero-knowledge: only ciphertext and operational metadata land here, never
// plaintext, keys, or capabilities. Two backends implement it: embedded SQLite
// (the default) and Postgres for production.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound covers anything missing or hard-deleted. Callers must treat
// expired/revoked/never-existed identically so we don't leak whether a slug ever existed.
var ErrNotFound = errors.New("store: not found")

// ErrExhausted means a view-limited paste has no views left.
var ErrExhausted = errors.New("store: view limit exhausted")

// ErrConflict is a unique-constraint collision (slug or short-code).
var ErrConflict = errors.New("store: conflict")

// ErrRoomFull means a live room already holds the maximum participants.
var ErrRoomFull = errors.New("store: room full")

// ErrFrozen means a live room is read-only and won't accept new updates.
var ErrFrozen = errors.New("store: room frozen")

// ErrStorageFull means appending would push a room past its storage cap.
var ErrStorageFull = errors.New("store: storage full")

// Paste is a stored static paste. The content lives in Ciphertext and the server
// can't read it; CryptoMetadata is the opaque public crypto params.
type Paste struct {
	ID              string
	Slug            string
	Ciphertext      []byte
	CryptoMetadata  []byte // opaque JSON: nonce, salt, argon2 params, version
	CreatedAt       time.Time
	ExpiresAt       time.Time
	BurnAfterRead   bool
	MaxViews        *int
	SuccessfulViews int
	LastViewedAt    *time.Time
	OwnerTokenHash  []byte
	ShortCode       *string
	RevokedAt       *time.Time
	ExhaustedAt     *time.Time
	CiphertextBytes int
}

// PasteMeta is public metadata you can fetch without consuming a view.
type PasteMeta struct {
	Slug                   string
	Available              bool
	RequiresExplicitReveal bool
	ExpiresAt              time.Time
	CryptoMetadata         []byte
}

// CreatePasteParams is everything needed to persist a new paste.
type CreatePasteParams struct {
	Slug           string
	Ciphertext     []byte
	CryptoMetadata []byte
	ExpiresAt      time.Time
	BurnAfterRead  bool
	MaxViews       *int
	OwnerTokenHash []byte
	ShortCode      *string
	// Kind is a lightweight, server-visible classification ("paste", "diff",
	// ...) set by the client at creation time — never inspected for content,
	// purely for operator-facing grouping. Empty means "paste".
	Kind string
}

// Room is a live collaboration room. Like a paste, the server only ever sees
// opaque encrypted blobs and capability hashes, never the room key or plaintext.
type Room struct {
	ID              string
	Slug            string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	EditorTokenHash []byte
	OwnerTokenHash  []byte
	CryptoMetadata  []byte
	State           string // "active" | "frozen" | "revoked"
	FrozenAt        *time.Time
	RevokedAt       *time.Time
	StorageBytes    int64
	NextSeq         int64
	ShortCode       *string
}

// Snapshot is a compacted encrypted room state. Seq is the base sequence: every
// update with seq <= Seq is already folded into this blob.
type Snapshot struct {
	Seq        int64
	Ciphertext []byte
	CreatedAt  time.Time
	Bytes      int
}

// Update is one opaque encrypted Yjs update with its per-room sequence number.
type Update struct {
	Seq        int64
	Ciphertext []byte
	CreatedAt  time.Time
	Bytes      int
}

// CreateRoomParams is everything needed to persist a new live room.
type CreateRoomParams struct {
	Slug            string
	EditorTokenHash []byte
	OwnerTokenHash  []byte
	CryptoMetadata  []byte
	ExpiresAt       time.Time
	ShortCode       *string
	InitialSnapshot []byte // optional seed snapshot (base_seq 0)
}

// Store is the persistence contract. Backends must enforce expiry at query time
// and give us a race-safe atomic redemption path.
type Store interface {
	Ping(ctx context.Context) error
	Migrate(ctx context.Context) error
	Close() error

	// CreatePaste returns ErrConflict on a slug collision.
	CreatePaste(ctx context.Context, p CreatePasteParams) (*Paste, error)
	// GetPasteMeta returns metadata without consuming a view, or ErrNotFound.
	GetPasteMeta(ctx context.Context, slug string) (*PasteMeta, error)
	// RedeemPaste atomically checks availability, bumps the view counter, marks
	// exhaustion, and hands back the ciphertext; safe under concurrent redemption.
	RedeemPaste(ctx context.Context, slug string) (*Paste, error)
	// RevokePaste revokes a paste if the owner hash matches. Idempotent; revoking
	// something already gone isn't an error.
	RevokePaste(ctx context.Context, slug string, ownerTokenHash []byte) error
	// GetOwnerStatus returns owner-visible status when the owner hash matches.
	GetOwnerStatus(ctx context.Context, slug string, ownerTokenHash []byte) (*Paste, error)

	// ResolveShortCode maps a short code to the internal path it points at:
	// "/p/{slug}" for a paste or "/live/{slug}" for a room, or ErrNotFound.
	ResolveShortCode(ctx context.Context, code string) (path string, err error)

	// DeleteExpired drops rows past their expiry (pastes and rooms, cascading to
	// room snapshots/updates) and returns how many went.
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)

	// --- live collaboration ---

	// CreateRoom returns ErrConflict on a slug/short-code collision.
	CreateRoom(ctx context.Context, p CreateRoomParams) (*Room, error)
	// GetRoomForEditor returns the room only if the editor hash checks out,
	// else ErrNotFound (mismatch and missing look identical).
	GetRoomForEditor(ctx context.Context, slug string, editorTokenHash []byte) (*Room, error)
	// GetRoomForOwner returns the room only if the owner hash checks out.
	GetRoomForOwner(ctx context.Context, slug string, ownerTokenHash []byte) (*Room, error)
	// AppendUpdate atomically assigns the next seq, adds the update, and grows the
	// storage counter. ErrStorageFull over cap, ErrFrozen if frozen, ErrNotFound if gone.
	AppendUpdate(ctx context.Context, slug string, ciphertext []byte, maxStorageBytes int64) (seq int64, err error)
	// BootstrapRoom returns the latest snapshot and every update past it, for a
	// room the caller has already fetched and authenticated via
	// GetRoomForEditor (avoids re-fetching the room row a second time).
	BootstrapRoom(ctx context.Context, room *Room) (snapshot *Snapshot, updates []Update, err error)
	// SaveSnapshot stores a new latest snapshot, prunes updates with seq <= baseSeq,
	// and recomputes storage_bytes.
	SaveSnapshot(ctx context.Context, slug string, ciphertext []byte, baseSeq int64) error
	// FreezeRoom flips a room read-only if the owner hash checks out.
	FreezeRoom(ctx context.Context, slug string, ownerTokenHash []byte) error
	// DeleteRoom revokes a room if the owner hash checks out. Idempotent.
	DeleteRoom(ctx context.Context, slug string, ownerTokenHash []byte) error
}
