package sqlite

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/720pRed/redpaste/internal/store"
)

func (s *Store) CreateRoom(ctx context.Context, p store.CreateRoomParams) (*store.Room, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite begin create room: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	id := newID()
	now := time.Now().UTC()
	storageBytes := int64(len(p.InitialSnapshot))
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_rooms (id, slug, created_at, expires_at, editor_token_hash,
			owner_token_hash, crypto_metadata, state, storage_bytes, next_seq, short_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, 1, ?)`,
		id, p.Slug, now, p.ExpiresAt.UTC(), p.EditorTokenHash, p.OwnerTokenHash,
		p.CryptoMetadata, storageBytes, p.ShortCode)
	if err != nil {
		if isUnique(err) {
			return nil, store.ErrConflict
		}
		return nil, fmt.Errorf("sqlite create room: %w", err)
	}

	// A seed snapshot lets the first joiner load state without replaying updates.
	if len(p.InitialSnapshot) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_snapshots (id, room_id, created_at, ciphertext, base_seq, bytes)
			VALUES (?, ?, ?, ?, 0, ?)`,
			newID(), id, now, p.InitialSnapshot, len(p.InitialSnapshot)); err != nil {
			return nil, fmt.Errorf("sqlite create room snapshot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite commit create room: %w", err)
	}
	return &store.Room{
		ID: id, Slug: p.Slug, CreatedAt: now, ExpiresAt: p.ExpiresAt.UTC(),
		EditorTokenHash: p.EditorTokenHash, OwnerTokenHash: p.OwnerTokenHash,
		CryptoMetadata: p.CryptoMetadata, State: "active",
		StorageBytes: storageBytes, NextSeq: 1, ShortCode: p.ShortCode,
	}, nil
}

// scanRoom loads a room by slug. Availability and capability checks live in callers.
func (s *Store) scanRoom(ctx context.Context, slug string) (*store.Room, error) {
	var (
		r         store.Room
		frozenAt  sql.NullTime
		revokedAt sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, created_at, expires_at, editor_token_hash, owner_token_hash,
			crypto_metadata, state, frozen_at, revoked_at, storage_bytes, next_seq
		FROM live_rooms WHERE slug = ?`, slug).
		Scan(&r.ID, &r.Slug, &r.CreatedAt, &r.ExpiresAt, &r.EditorTokenHash, &r.OwnerTokenHash,
			&r.CryptoMetadata, &r.State, &frozenAt, &revokedAt, &r.StorageBytes, &r.NextSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite scan room: %w", err)
	}
	if frozenAt.Valid {
		t := frozenAt.Time.UTC()
		r.FrozenAt = &t
	}
	if revokedAt.Valid {
		t := revokedAt.Time.UTC()
		r.RevokedAt = &t
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("sqlite begin append: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the room row for the seq bump so concurrent writers can't collide.
	var (
		roomID       string
		expiresAt    time.Time
		state        string
		storageBytes int64
		nextSeq      int64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, expires_at, state, storage_bytes, next_seq FROM live_rooms WHERE slug = ?`, slug).
		Scan(&roomID, &expiresAt, &state, &storageBytes, &nextSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("sqlite append select: %w", err)
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
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_updates (id, room_id, seq, created_at, ciphertext, bytes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), roomID, seq, now, ciphertext, len(ciphertext)); err != nil {
		return 0, fmt.Errorf("sqlite append insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_rooms SET next_seq = next_seq + 1, storage_bytes = storage_bytes + ? WHERE id = ?`,
		len(ciphertext), roomID); err != nil {
		return 0, fmt.Errorf("sqlite append bump: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("sqlite commit append: %w", err)
	}
	return seq, nil
}

func (s *Store) BootstrapRoom(ctx context.Context, room *store.Room) (*store.Snapshot, []store.Update, error) {
	var (
		snap    store.Snapshot
		baseSeq int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT ciphertext, base_seq, created_at, bytes FROM live_snapshots
		WHERE room_id = ? ORDER BY base_seq DESC LIMIT 1`, room.ID).
		Scan(&snap.Ciphertext, &snap.Seq, &snap.CreatedAt, &snap.Bytes)
	var snapshot *store.Snapshot
	switch {
	case err == nil:
		baseSeq = snap.Seq
		snapshot = &snap
	case errors.Is(err, sql.ErrNoRows):
		baseSeq = 0
	default:
		return nil, nil, fmt.Errorf("sqlite bootstrap snapshot: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, ciphertext, created_at, bytes FROM live_updates
		WHERE room_id = ? AND seq > ? ORDER BY seq ASC`, room.ID, baseSeq)
	if err != nil {
		return nil, nil, fmt.Errorf("sqlite bootstrap updates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var updates []store.Update
	for rows.Next() {
		var u store.Update
		if err := rows.Scan(&u.Seq, &u.Ciphertext, &u.CreatedAt, &u.Bytes); err != nil {
			return nil, nil, fmt.Errorf("sqlite bootstrap scan: %w", err)
		}
		updates = append(updates, u)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("sqlite bootstrap rows: %w", err)
	}
	return snapshot, updates, nil
}

func (s *Store) SaveSnapshot(ctx context.Context, slug string, ciphertext []byte, baseSeq int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite begin snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		roomID    string
		expiresAt time.Time
		state     string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, expires_at, state FROM live_rooms WHERE slug = ?`, slug).
		Scan(&roomID, &expiresAt, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("sqlite snapshot select: %w", err)
	}
	if !roomAvailable(expiresAt, state) {
		return store.ErrNotFound
	}

	now := time.Now().UTC()
	// Keep a single latest snapshot per room.
	if _, err := tx.ExecContext(ctx, `DELETE FROM live_snapshots WHERE room_id = ?`, roomID); err != nil {
		return fmt.Errorf("sqlite snapshot clear: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_snapshots (id, room_id, created_at, ciphertext, base_seq, bytes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), roomID, now, ciphertext, baseSeq, len(ciphertext)); err != nil {
		return fmt.Errorf("sqlite snapshot insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM live_updates WHERE room_id = ? AND seq <= ?`, roomID, baseSeq); err != nil {
		return fmt.Errorf("sqlite snapshot prune: %w", err)
	}

	// Recompute storage from what's actually left.
	var updBytes sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(bytes), 0) FROM live_updates WHERE room_id = ?`, roomID).Scan(&updBytes); err != nil {
		return fmt.Errorf("sqlite snapshot sum: %w", err)
	}
	total := updBytes.Int64 + int64(len(ciphertext))
	if _, err := tx.ExecContext(ctx,
		`UPDATE live_rooms SET storage_bytes = ? WHERE id = ?`, total, roomID); err != nil {
		return fmt.Errorf("sqlite snapshot storage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite commit snapshot: %w", err)
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
	_, err = s.db.ExecContext(ctx, `
		UPDATE live_rooms SET state = 'frozen', frozen_at = ? WHERE id = ? AND state = 'active'`,
		time.Now().UTC(), r.ID)
	if err != nil {
		return fmt.Errorf("sqlite freeze: %w", err)
	}
	return nil
}

func (s *Store) DeleteRoom(ctx context.Context, slug string, ownerTokenHash []byte) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE live_rooms SET state = 'revoked', revoked_at = ?
		WHERE slug = ? AND owner_token_hash = ? AND state != 'revoked'`,
		time.Now().UTC(), slug, ownerTokenHash)
	if err != nil {
		return fmt.Errorf("sqlite delete room: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Distinguish "already revoked / gone" from "wrong owner" without leaking.
		var exists int
		err := s.db.QueryRowContext(ctx,
			`SELECT 1 FROM live_rooms WHERE slug = ? AND owner_token_hash = ?`, slug, ownerTokenHash).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}
	}
	return nil
}
