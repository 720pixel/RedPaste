package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/720pRed/redpaste/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) CreateRoom(ctx context.Context, p store.CreateRoomParams) (*store.Room, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres begin create room: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	id := newID()
	now := time.Now().UTC()
	storageBytes := int64(len(p.InitialSnapshot))
	_, err = tx.Exec(ctx, `
		INSERT INTO live_rooms (id, slug, created_at, expires_at, editor_token_hash,
			owner_token_hash, crypto_metadata, state, storage_bytes, next_seq, short_code)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'active',$8,1,$9)`,
		id, p.Slug, now, p.ExpiresAt.UTC(), p.EditorTokenHash, p.OwnerTokenHash,
		p.CryptoMetadata, storageBytes, p.ShortCode)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, store.ErrConflict
		}
		return nil, fmt.Errorf("postgres create room: %w", err)
	}

	if len(p.InitialSnapshot) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO live_snapshots (id, room_id, created_at, ciphertext, base_seq, bytes)
			VALUES ($1,$2,$3,$4,0,$5)`,
			newID(), id, now, p.InitialSnapshot, len(p.InitialSnapshot)); err != nil {
			return nil, fmt.Errorf("postgres create room snapshot: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres commit create room: %w", err)
	}
	return &store.Room{
		ID: id, Slug: p.Slug, CreatedAt: now, ExpiresAt: p.ExpiresAt.UTC(),
		EditorTokenHash: p.EditorTokenHash, OwnerTokenHash: p.OwnerTokenHash,
		CryptoMetadata: p.CryptoMetadata, State: "active",
		StorageBytes: storageBytes, NextSeq: 1, ShortCode: p.ShortCode,
	}, nil
}

func (s *Store) scanRoom(ctx context.Context, slug string) (*store.Room, error) {
	var r store.Room
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, created_at, expires_at, editor_token_hash, owner_token_hash,
			crypto_metadata, state, frozen_at, revoked_at, storage_bytes, next_seq
		FROM live_rooms WHERE slug = $1`, slug).
		Scan(&r.ID, &r.Slug, &r.CreatedAt, &r.ExpiresAt, &r.EditorTokenHash, &r.OwnerTokenHash,
			&r.CryptoMetadata, &r.State, &r.FrozenAt, &r.RevokedAt, &r.StorageBytes, &r.NextSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres scan room: %w", err)
	}
	return &r, nil
}

func (s *Store) GetRoomForEditor(ctx context.Context, slug string, editorTokenHash []byte) (*store.Room, error) {
	r, err := s.scanRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(r.EditorTokenHash, editorTokenHash) != 1 || !roomAvailable(r.ExpiresAt, r.State) {
		return nil, store.ErrNotFound
	}
	return r, nil
}

func (s *Store) GetRoomForOwner(ctx context.Context, slug string, ownerTokenHash []byte) (*store.Room, error) {
	r, err := s.scanRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	// Owner keeps access to a frozen room; only revoked/expired reads as gone.
	if subtle.ConstantTimeCompare(r.OwnerTokenHash, ownerTokenHash) != 1 || !roomAvailable(r.ExpiresAt, r.State) {
		return nil, store.ErrNotFound
	}
	return r, nil
}

func (s *Store) AppendUpdate(ctx context.Context, slug string, ciphertext []byte, maxStorageBytes int64) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("postgres begin append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// FOR UPDATE locks the room row so concurrent writers can't grab the same seq.
	var (
		roomID       string
		expiresAt    time.Time
		state        string
		storageBytes int64
		nextSeq      int64
	)
	err = tx.QueryRow(ctx, `
		SELECT id, expires_at, state, storage_bytes, next_seq FROM live_rooms WHERE slug = $1 FOR UPDATE`, slug).
		Scan(&roomID, &expiresAt, &state, &storageBytes, &nextSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("postgres append select: %w", err)
	}
	if !roomAvailable(expiresAt, state) {
		return 0, store.ErrNotFound
	}
	if state == "frozen" {
		return 0, store.ErrFrozen
	}
	if storageBytes+int64(len(ciphertext)) > maxStorageBytes {
		return 0, store.ErrStorageFull
	}

	seq := nextSeq
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		INSERT INTO live_updates (id, room_id, seq, created_at, ciphertext, bytes)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		newID(), roomID, seq, now, ciphertext, len(ciphertext)); err != nil {
		return 0, fmt.Errorf("postgres append insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE live_rooms SET next_seq = next_seq + 1, storage_bytes = storage_bytes + $1 WHERE id = $2`,
		len(ciphertext), roomID); err != nil {
		return 0, fmt.Errorf("postgres append bump: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("postgres commit append: %w", err)
	}
	return seq, nil
}

func (s *Store) BootstrapRoom(ctx context.Context, room *store.Room) (*store.Snapshot, []store.Update, error) {
	var (
		snap    store.Snapshot
		baseSeq int64
	)
	err := s.pool.QueryRow(ctx, `
		SELECT ciphertext, base_seq, created_at, bytes FROM live_snapshots
		WHERE room_id = $1 ORDER BY base_seq DESC LIMIT 1`, room.ID).
		Scan(&snap.Ciphertext, &snap.Seq, &snap.CreatedAt, &snap.Bytes)
	var snapshot *store.Snapshot
	switch {
	case err == nil:
		baseSeq = snap.Seq
		snapshot = &snap
	case errors.Is(err, pgx.ErrNoRows):
		baseSeq = 0
	default:
		return nil, nil, fmt.Errorf("postgres bootstrap snapshot: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT seq, ciphertext, created_at, bytes FROM live_updates
		WHERE room_id = $1 AND seq > $2 ORDER BY seq ASC`, room.ID, baseSeq)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres bootstrap updates: %w", err)
	}
	defer rows.Close()

	var updates []store.Update
	for rows.Next() {
		var u store.Update
		if err := rows.Scan(&u.Seq, &u.Ciphertext, &u.CreatedAt, &u.Bytes); err != nil {
			return nil, nil, fmt.Errorf("postgres bootstrap scan: %w", err)
		}
		updates = append(updates, u)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("postgres bootstrap rows: %w", err)
	}
	return snapshot, updates, nil
}

func (s *Store) SaveSnapshot(ctx context.Context, slug string, ciphertext []byte, baseSeq int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres begin snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		roomID    string
		expiresAt time.Time
		state     string
	)
	err = tx.QueryRow(ctx, `
		SELECT id, expires_at, state FROM live_rooms WHERE slug = $1 FOR UPDATE`, slug).
		Scan(&roomID, &expiresAt, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("postgres snapshot select: %w", err)
	}
	if !roomAvailable(expiresAt, state) {
		return store.ErrNotFound
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `DELETE FROM live_snapshots WHERE room_id = $1`, roomID); err != nil {
		return fmt.Errorf("postgres snapshot clear: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO live_snapshots (id, room_id, created_at, ciphertext, base_seq, bytes)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		newID(), roomID, now, ciphertext, baseSeq, len(ciphertext)); err != nil {
		return fmt.Errorf("postgres snapshot insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM live_updates WHERE room_id = $1 AND seq <= $2`, roomID, baseSeq); err != nil {
		return fmt.Errorf("postgres snapshot prune: %w", err)
	}

	var updBytes int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(bytes), 0) FROM live_updates WHERE room_id = $1`, roomID).Scan(&updBytes); err != nil {
		return fmt.Errorf("postgres snapshot sum: %w", err)
	}
	total := updBytes + int64(len(ciphertext))
	if _, err := tx.Exec(ctx,
		`UPDATE live_rooms SET storage_bytes = $1 WHERE id = $2`, total, roomID); err != nil {
		return fmt.Errorf("postgres snapshot storage: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres commit snapshot: %w", err)
	}
	return nil
}

func (s *Store) FreezeRoom(ctx context.Context, slug string, ownerTokenHash []byte) error {
	r, err := s.GetRoomForOwner(ctx, slug, ownerTokenHash)
	if err != nil {
		return err
	}
	if r.State == "frozen" {
		return nil // idempotent
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE live_rooms SET state = 'frozen', frozen_at = $1 WHERE id = $2 AND state = 'active'`,
		time.Now().UTC(), r.ID)
	if err != nil {
		return fmt.Errorf("postgres freeze: %w", err)
	}
	return nil
}

func (s *Store) DeleteRoom(ctx context.Context, slug string, ownerTokenHash []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE live_rooms SET state = 'revoked', revoked_at = $1
		WHERE slug = $2 AND owner_token_hash = $3 AND state != 'revoked'`,
		time.Now().UTC(), slug, ownerTokenHash)
	if err != nil {
		return fmt.Errorf("postgres delete room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists int
		err := s.pool.QueryRow(ctx,
			`SELECT 1 FROM live_rooms WHERE slug = $1 AND owner_token_hash = $2`, slug, ownerTokenHash).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
	}
	return nil
}
