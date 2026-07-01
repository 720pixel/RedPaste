// Package postgres is the store.Store backend on Postgres (pgx/v5), used in prod
// when REDPASTE_DATABASE_URL is set.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/720pRed/redpaste/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the Postgres-backed store.Store.
type Store struct {
	pool *pgxpool.Pool
}

const schema = `
CREATE TABLE IF NOT EXISTS pastes (
	id                TEXT PRIMARY KEY,
	slug              TEXT UNIQUE NOT NULL,
	ciphertext        BYTEA NOT NULL,
	crypto_metadata   BYTEA NOT NULL,
	created_at        TIMESTAMPTZ NOT NULL,
	expires_at        TIMESTAMPTZ NOT NULL,
	burn_after_read   BOOLEAN NOT NULL DEFAULT false,
	max_views         INTEGER,
	successful_views  INTEGER NOT NULL DEFAULT 0,
	last_viewed_at    TIMESTAMPTZ,
	owner_token_hash  BYTEA NOT NULL,
	short_code        TEXT,
	revoked_at        TIMESTAMPTZ,
	exhausted_at      TIMESTAMPTZ,
	ciphertext_bytes  INTEGER NOT NULL,
	kind              TEXT NOT NULL DEFAULT 'paste',
	CONSTRAINT expires_after_created CHECK (expires_at > created_at),
	CONSTRAINT max_views_positive CHECK (max_views IS NULL OR max_views >= 1),
	CONSTRAINT views_nonneg CHECK (successful_views >= 0)
);
CREATE INDEX IF NOT EXISTS idx_pastes_expires_at ON pastes(expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pastes_short_code ON pastes(short_code) WHERE short_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS live_rooms (
	id                TEXT PRIMARY KEY,
	slug              TEXT UNIQUE NOT NULL,
	created_at        TIMESTAMPTZ NOT NULL,
	expires_at        TIMESTAMPTZ NOT NULL,
	editor_token_hash BYTEA NOT NULL,
	owner_token_hash  BYTEA NOT NULL,
	crypto_metadata   BYTEA NOT NULL,
	state             TEXT NOT NULL DEFAULT 'active',
	frozen_at         TIMESTAMPTZ,
	revoked_at        TIMESTAMPTZ,
	storage_bytes     BIGINT NOT NULL DEFAULT 0,
	next_seq          BIGINT NOT NULL DEFAULT 1,
	short_code        TEXT,
	CONSTRAINT room_expires_after_created CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS idx_live_rooms_expires_at ON live_rooms(expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_live_rooms_short_code ON live_rooms(short_code) WHERE short_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS live_snapshots (
	id          TEXT PRIMARY KEY,
	room_id     TEXT NOT NULL,
	created_at  TIMESTAMPTZ NOT NULL,
	ciphertext  BYTEA NOT NULL,
	base_seq    BIGINT NOT NULL,
	bytes       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_live_snapshots_room ON live_snapshots(room_id);

CREATE TABLE IF NOT EXISTS live_updates (
	id          TEXT PRIMARY KEY,
	room_id     TEXT NOT NULL,
	seq         BIGINT NOT NULL,
	created_at  TIMESTAMPTZ NOT NULL,
	ciphertext  BYTEA NOT NULL,
	bytes       INTEGER NOT NULL,
	UNIQUE(room_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_live_updates_room ON live_updates(room_id, seq);
`

// Open connects and sets up a bounded connection pool.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres parse config: %w", err)
	}
	cfg.MaxConns = 25
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *Store) Close() error                   { s.pool.Close(); return nil }

// Raw exposes the underlying connection for tooling that needs direct SQL
// access beyond this interface (operator scripts, schema introspection). The
// concrete type is backend-specific: *pgxpool.Pool here, *sql.DB for SQLite.
func (s *Store) Raw() any { return s.pool }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("postgres migrate: %w", err)
	}
	// Additive migration for DBs created before the `kind` column existed.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE pastes ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'paste'`); err != nil {
		return fmt.Errorf("postgres migrate (kind column): %w", err)
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
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pastes (id, slug, ciphertext, crypto_metadata, created_at, expires_at,
			burn_after_read, max_views, owner_token_hash, short_code, ciphertext_bytes, kind)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		id, p.Slug, p.Ciphertext, p.CryptoMetadata, now, p.ExpiresAt.UTC(),
		p.BurnAfterRead, p.MaxViews, p.OwnerTokenHash, p.ShortCode, len(p.Ciphertext), kind)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, store.ErrConflict
		}
		return nil, fmt.Errorf("postgres create paste: %w", err)
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
		maxViews    *int
		revokedAt   *time.Time
		exhaustedAt *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT crypto_metadata, expires_at, burn_after_read, max_views, revoked_at, exhausted_at
		FROM pastes WHERE slug = $1`, slug).
		Scan(&meta, &expiresAt, &burn, &maxViews, &revokedAt, &exhaustedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres get meta: %w", err)
	}
	if !available(expiresAt, revokedAt, exhaustedAt) {
		return nil, store.ErrNotFound
	}
	return &store.PasteMeta{
		Slug:                   slug,
		Available:              true,
		RequiresExplicitReveal: burn || maxViews != nil,
		ExpiresAt:              expiresAt.UTC(),
		CryptoMetadata:         meta,
	}, nil
}

// RedeemPaste consumes a view atomically. SELECT ... FOR UPDATE locks the row so
// two racing requests can't both grab the last view.
func (s *Store) RedeemPaste(ctx context.Context, slug string) (*store.Paste, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		p           store.Paste
		burn        bool
		maxViews    *int
		revokedAt   *time.Time
		exhaustedAt *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT id, ciphertext, crypto_metadata, expires_at, burn_after_read, max_views,
			successful_views, revoked_at, exhausted_at
		FROM pastes WHERE slug = $1 FOR UPDATE`, slug).
		Scan(&p.ID, &p.Ciphertext, &p.CryptoMetadata, &p.ExpiresAt, &burn, &maxViews,
			&p.SuccessfulViews, &revokedAt, &exhaustedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres redeem select: %w", err)
	}
	if !available(p.ExpiresAt, revokedAt, exhaustedAt) {
		return nil, store.ErrNotFound
	}
	if maxViews != nil && p.SuccessfulViews >= *maxViews {
		return nil, store.ErrExhausted
	}

	now := time.Now().UTC()
	newViews := p.SuccessfulViews + 1
	// burn-after-read is a one-shot: exhaust it the moment it's read.
	var exhaustVal *time.Time
	if burn || (maxViews != nil && newViews >= *maxViews) {
		exhaustVal = &now
	}
	if _, err := tx.Exec(ctx, `
		UPDATE pastes SET successful_views = $1, last_viewed_at = $2, exhausted_at = $3
		WHERE slug = $4`, newViews, now, exhaustVal, slug); err != nil {
		return nil, fmt.Errorf("postgres redeem update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres redeem commit: %w", err)
	}
	p.Slug = slug
	p.SuccessfulViews = newViews
	return &p, nil
}

func (s *Store) RevokePaste(ctx context.Context, slug string, ownerTokenHash []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE pastes SET revoked_at = $1
		WHERE slug = $2 AND owner_token_hash = $3 AND revoked_at IS NULL`,
		time.Now().UTC(), slug, ownerTokenHash)
	if err != nil {
		return fmt.Errorf("postgres revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists int
		err := s.pool.QueryRow(ctx,
			`SELECT 1 FROM pastes WHERE slug = $1 AND owner_token_hash = $2`, slug, ownerTokenHash).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
	}
	return nil
}

func (s *Store) GetOwnerStatus(ctx context.Context, slug string, ownerTokenHash []byte) (*store.Paste, error) {
	var p store.Paste
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, created_at, expires_at, burn_after_read, max_views,
			successful_views, last_viewed_at, revoked_at, exhausted_at, ciphertext_bytes
		FROM pastes WHERE slug = $1 AND owner_token_hash = $2`, slug, ownerTokenHash).
		Scan(&p.ID, &p.Slug, &p.CreatedAt, &p.ExpiresAt, &p.BurnAfterRead, &p.MaxViews,
			&p.SuccessfulViews, &p.LastViewedAt, &p.RevokedAt, &p.ExhaustedAt, &p.CiphertextBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres owner status: %w", err)
	}
	return &p, nil
}

// ResolveShortCode checks pastes first, then live rooms, and hands back the
// internal path so the client can redirect while keeping its fragment secret.
func (s *Store) ResolveShortCode(ctx context.Context, code string) (string, error) {
	var (
		slug        string
		expiresAt   time.Time
		revokedAt   *time.Time
		exhaustedAt *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT slug, expires_at, revoked_at, exhausted_at FROM pastes WHERE short_code = $1`, code).
		Scan(&slug, &expiresAt, &revokedAt, &exhaustedAt)
	if err == nil {
		if !available(expiresAt, revokedAt, exhaustedAt) {
			return "", store.ErrNotFound
		}
		return "/p/" + slug, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("postgres resolve short code: %w", err)
	}

	var (
		roomSlug  string
		roomExp   time.Time
		roomState string
	)
	err = s.pool.QueryRow(ctx, `
		SELECT slug, expires_at, state FROM live_rooms WHERE short_code = $1`, code).
		Scan(&roomSlug, &roomExp, &roomState)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", store.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("postgres resolve short code: %w", err)
	}
	if !roomAvailable(roomExp, roomState) {
		return "", store.ErrNotFound
	}
	return "/live/" + roomSlug, nil
}

func (s *Store) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("postgres begin delete expired: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`DELETE FROM pastes WHERE expires_at <= $1 OR revoked_at IS NOT NULL`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("postgres delete expired pastes: %w", err)
	}
	n := tag.RowsAffected()

	rtag, err := tx.Exec(ctx,
		`DELETE FROM live_rooms WHERE expires_at <= $1 OR revoked_at IS NOT NULL`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("postgres delete expired rooms: %w", err)
	}
	rn := rtag.RowsAffected()
	if _, err := tx.Exec(ctx,
		`DELETE FROM live_snapshots WHERE room_id NOT IN (SELECT id FROM live_rooms)`); err != nil {
		return 0, fmt.Errorf("postgres prune snapshots: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM live_updates WHERE room_id NOT IN (SELECT id FROM live_rooms)`); err != nil {
		return 0, fmt.Errorf("postgres prune updates: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("postgres commit delete expired: %w", err)
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

func available(expiresAt time.Time, revokedAt, exhaustedAt *time.Time) bool {
	if time.Now().UTC().After(expiresAt.UTC()) {
		return false
	}
	return revokedAt == nil && exhaustedAt == nil
}
