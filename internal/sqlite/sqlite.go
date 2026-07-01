// Package sqlite is the store.Store backend on embedded pure-Go SQLite
// (modernc.org/sqlite, no CGO), the default, so the whole thing ships as one binary.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/720pRed/redpaste/internal/store"
	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed store.Store.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS pastes (
	id                TEXT PRIMARY KEY,
	slug              TEXT UNIQUE NOT NULL,
	ciphertext        BLOB NOT NULL,
	crypto_metadata   BLOB NOT NULL,
	created_at        TIMESTAMP NOT NULL,
	expires_at        TIMESTAMP NOT NULL,
	burn_after_read   INTEGER NOT NULL DEFAULT 0,
	max_views         INTEGER,
	successful_views  INTEGER NOT NULL DEFAULT 0,
	last_viewed_at    TIMESTAMP,
	owner_token_hash  BLOB NOT NULL,
	short_code        TEXT,
	revoked_at        TIMESTAMP,
	exhausted_at      TIMESTAMP,
	ciphertext_bytes  INTEGER NOT NULL,
	kind              TEXT NOT NULL DEFAULT 'paste',
	CHECK (expires_at > created_at),
	CHECK (max_views IS NULL OR max_views >= 1),
	CHECK (successful_views >= 0)
);
CREATE INDEX IF NOT EXISTS idx_pastes_expires_at ON pastes(expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pastes_short_code ON pastes(short_code) WHERE short_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS live_rooms (
	id                TEXT PRIMARY KEY,
	slug              TEXT UNIQUE NOT NULL,
	created_at        TIMESTAMP NOT NULL,
	expires_at        TIMESTAMP NOT NULL,
	editor_token_hash BLOB NOT NULL,
	owner_token_hash  BLOB NOT NULL,
	crypto_metadata   BLOB NOT NULL,
	state             TEXT NOT NULL DEFAULT 'active',
	frozen_at         TIMESTAMP,
	revoked_at        TIMESTAMP,
	storage_bytes     INTEGER NOT NULL DEFAULT 0,
	next_seq          INTEGER NOT NULL DEFAULT 1,
	short_code        TEXT,
	CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS idx_live_rooms_expires_at ON live_rooms(expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_live_rooms_short_code ON live_rooms(short_code) WHERE short_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS live_snapshots (
	id          TEXT PRIMARY KEY,
	room_id     TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL,
	ciphertext  BLOB NOT NULL,
	base_seq    INTEGER NOT NULL,
	bytes       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_live_snapshots_room ON live_snapshots(room_id);

CREATE TABLE IF NOT EXISTS live_updates (
	id          TEXT PRIMARY KEY,
	room_id     TEXT NOT NULL,
	seq         INTEGER NOT NULL,
	created_at  TIMESTAMP NOT NULL,
	ciphertext  BLOB NOT NULL,
	bytes       INTEGER NOT NULL,
	UNIQUE(room_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_live_updates_room ON live_updates(room_id, seq);
`

// Open opens or creates the DB at path, tuned for safe concurrent access.
func Open(ctx context.Context, path string) (*Store, error) {
	// WAL + a busy timeout keep concurrent readers and writers from stepping on each other.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	// SQLite serializes writes anyway; one connection sidesteps "database is locked".
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                   { return s.db.Close() }

// Raw exposes the underlying connection for tooling that needs direct SQL
// access beyond this interface (operator scripts, schema introspection). The
// concrete type is backend-specific: *sql.DB here, *pgxpool.Pool for Postgres.
func (s *Store) Raw() any { return s.db }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("sqlite migrate: %w", err)
	}
	// Additive migration for DBs created before the `kind` column existed;
	// CREATE TABLE IF NOT EXISTS above is a no-op against an existing table.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE pastes ADD COLUMN kind TEXT NOT NULL DEFAULT 'paste'`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("sqlite migrate (kind column): %w", err)
		}
	}
	return nil
}

func (s *Store) CreatePaste(ctx context.Context, p store.CreatePasteParams) (*store.Paste, error) {
	id := newID()
	now := time.Now().UTC()
	kind := p.Kind
	if kind == "" {
		kind = "paste"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pastes (id, slug, ciphertext, crypto_metadata, created_at, expires_at,
			burn_after_read, max_views, owner_token_hash, short_code, ciphertext_bytes, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.Slug, p.Ciphertext, p.CryptoMetadata, now, p.ExpiresAt.UTC(),
		p.BurnAfterRead, p.MaxViews, p.OwnerTokenHash, p.ShortCode, len(p.Ciphertext), kind)
	if err != nil {
		if isUnique(err) {
			return nil, store.ErrConflict
		}
		return nil, fmt.Errorf("sqlite create paste: %w", err)
	}
	return &store.Paste{
		ID: id, Slug: p.Slug, Ciphertext: p.Ciphertext, CryptoMetadata: p.CryptoMetadata,
		CreatedAt: now, ExpiresAt: p.ExpiresAt.UTC(), BurnAfterRead: p.BurnAfterRead,
		MaxViews: p.MaxViews, OwnerTokenHash: p.OwnerTokenHash, ShortCode: p.ShortCode,
		CiphertextBytes: len(p.Ciphertext),
	}, nil
}

func (s *Store) GetPasteMeta(ctx context.Context, slug string) (*store.PasteMeta, error) {
	var (
		meta        []byte
		expiresAt   time.Time
		burn        bool
		maxViews    sql.NullInt64
		revokedAt   sql.NullTime
		exhaustedAt sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT crypto_metadata, expires_at, burn_after_read, max_views, revoked_at, exhausted_at
		FROM pastes WHERE slug = ?`, slug).
		Scan(&meta, &expiresAt, &burn, &maxViews, &revokedAt, &exhaustedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite get meta: %w", err)
	}
	if !available(expiresAt, revokedAt, exhaustedAt) {
		return nil, store.ErrNotFound
	}
	return &store.PasteMeta{
		Slug:                   slug,
		Available:              true,
		RequiresExplicitReveal: burn || maxViews.Valid,
		ExpiresAt:              expiresAt.UTC(),
		CryptoMetadata:         meta,
	}, nil
}

// RedeemPaste consumes a view atomically so racing requests can't both take the last one.
func (s *Store) RedeemPaste(ctx context.Context, slug string) (*store.Paste, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		p           store.Paste
		burn        bool
		maxViews    sql.NullInt64
		revokedAt   sql.NullTime
		exhaustedAt sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, ciphertext, crypto_metadata, expires_at, burn_after_read, max_views,
			successful_views, revoked_at, exhausted_at
		FROM pastes WHERE slug = ?`, slug).
		Scan(&p.ID, &p.Ciphertext, &p.CryptoMetadata, &p.ExpiresAt, &burn, &maxViews,
			&p.SuccessfulViews, &revokedAt, &exhaustedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite redeem select: %w", err)
	}
	if !available(p.ExpiresAt, revokedAt, exhaustedAt) {
		return nil, store.ErrNotFound
	}
	if maxViews.Valid && int64(p.SuccessfulViews) >= maxViews.Int64 {
		return nil, store.ErrExhausted
	}

	now := time.Now().UTC()
	newViews := p.SuccessfulViews + 1
	// burn-after-read is a one-shot: exhaust it the moment it's read.
	exhaust := burn || (maxViews.Valid && int64(newViews) >= maxViews.Int64)
	var exhaustVal any
	if exhaust {
		exhaustVal = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pastes SET successful_views = ?, last_viewed_at = ?, exhausted_at = ?
		WHERE slug = ?`, newViews, now, exhaustVal, slug); err != nil {
		return nil, fmt.Errorf("sqlite redeem update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite redeem commit: %w", err)
	}
	p.Slug = slug
	p.SuccessfulViews = newViews
	return &p, nil
}

func (s *Store) RevokePaste(ctx context.Context, slug string, ownerTokenHash []byte) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pastes SET revoked_at = ?
		WHERE slug = ? AND owner_token_hash = ? AND revoked_at IS NULL`,
		time.Now().UTC(), slug, ownerTokenHash)
	if err != nil {
		return fmt.Errorf("sqlite revoke: %w", err)
	}
	// No row updated could just mean it's already revoked or gone. Check the owner
	// hash still matches a real row; otherwise return not-found so we don't leak existence.
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		err := s.db.QueryRowContext(ctx,
			`SELECT 1 FROM pastes WHERE slug = ? AND owner_token_hash = ?`, slug, ownerTokenHash).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}
	}
	return nil
}

func (s *Store) GetOwnerStatus(ctx context.Context, slug string, ownerTokenHash []byte) (*store.Paste, error) {
	var (
		p           store.Paste
		maxViews    sql.NullInt64
		lastViewed  sql.NullTime
		revokedAt   sql.NullTime
		exhaustedAt sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, created_at, expires_at, burn_after_read, max_views,
			successful_views, last_viewed_at, revoked_at, exhausted_at, ciphertext_bytes
		FROM pastes WHERE slug = ? AND owner_token_hash = ?`, slug, ownerTokenHash).
		Scan(&p.ID, &p.Slug, &p.CreatedAt, &p.ExpiresAt, &p.BurnAfterRead, &maxViews,
			&p.SuccessfulViews, &lastViewed, &revokedAt, &exhaustedAt, &p.CiphertextBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite owner status: %w", err)
	}
	if maxViews.Valid {
		v := int(maxViews.Int64)
		p.MaxViews = &v
	}
	if lastViewed.Valid {
		t := lastViewed.Time.UTC()
		p.LastViewedAt = &t
	}
	if revokedAt.Valid {
		t := revokedAt.Time.UTC()
		p.RevokedAt = &t
	}
	if exhaustedAt.Valid {
		t := exhaustedAt.Time.UTC()
		p.ExhaustedAt = &t
	}
	return &p, nil
}

// ResolveShortCode checks pastes first, then live rooms, and hands back the
// internal path so the client can redirect while keeping its fragment secret.
func (s *Store) ResolveShortCode(ctx context.Context, code string) (string, error) {
	var (
		slug        string
		expiresAt   time.Time
		revokedAt   sql.NullTime
		exhaustedAt sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT slug, expires_at, revoked_at, exhausted_at FROM pastes WHERE short_code = ?`, code).
		Scan(&slug, &expiresAt, &revokedAt, &exhaustedAt)
	if err == nil {
		if !available(expiresAt, revokedAt, exhaustedAt) {
			return "", store.ErrNotFound
		}
		return "/p/" + slug, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("sqlite resolve short code: %w", err)
	}

	var (
		roomSlug  string
		roomExp   time.Time
		roomState string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT slug, expires_at, state FROM live_rooms WHERE short_code = ?`, code).
		Scan(&roomSlug, &roomExp, &roomState)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("sqlite resolve short code: %w", err)
	}
	if !roomAvailable(roomExp, roomState) {
		return "", store.ErrNotFound
	}
	return "/live/" + roomSlug, nil
}

func (s *Store) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("sqlite begin delete expired: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`DELETE FROM pastes WHERE expires_at <= ? OR revoked_at IS NOT NULL`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("sqlite delete expired pastes: %w", err)
	}
	n, _ := res.RowsAffected()

	// Cascade: drop dead rooms first, then any orphaned snapshots/updates.
	rres, err := tx.ExecContext(ctx,
		`DELETE FROM live_rooms WHERE expires_at <= ? OR revoked_at IS NOT NULL`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("sqlite delete expired rooms: %w", err)
	}
	rn, _ := rres.RowsAffected()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM live_snapshots WHERE room_id NOT IN (SELECT id FROM live_rooms)`); err != nil {
		return 0, fmt.Errorf("sqlite prune snapshots: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM live_updates WHERE room_id NOT IN (SELECT id FROM live_rooms)`); err != nil {
		return 0, fmt.Errorf("sqlite prune updates: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("sqlite commit delete expired: %w", err)
	}
	return n + rn, nil
}

// roomAvailable reports whether a room can still be joined right now.
func roomAvailable(expiresAt time.Time, state string) bool {
	if time.Now().UTC().After(expiresAt.UTC()) {
		return false
	}
	return state != "revoked"
}

// available reports whether a paste can still be redeemed right now.
func available(expiresAt time.Time, revokedAt, exhaustedAt sql.NullTime) bool {
	if time.Now().UTC().After(expiresAt.UTC()) {
		return false
	}
	if revokedAt.Valid || exhaustedAt.Valid {
		return false
	}
	return true
}
