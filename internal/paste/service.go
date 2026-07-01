// Package paste is the business logic for static pastes: create, fetch metadata,
// redeem, owner status, delete. It sits between the HTTP layer and the store and
// owns policy enforcement (size, retention, view limits).
package paste

import (
	"context"
	"errors"
	"time"

	"github.com/720pRed/redpaste/internal/cryptoformat"
	"github.com/720pRed/redpaste/internal/store"
)

// shortest allowed lifetime; stops someone creating a paste that's already dead.
const minRetention = 60 * time.Second

var (
	ErrTooLarge      = errors.New("ciphertext too large")
	ErrBadRetention  = errors.New("retention out of range")
	ErrBadMaxViews   = errors.New("max views out of range")
	ErrBadMetadata   = errors.New("bad crypto metadata")
	ErrBadOwnerToken = errors.New("bad owner token")
	ErrUnavailable   = errors.New("paste unavailable")
	ErrForbidden     = errors.New("forbidden")
)

// Limits are the policy knobs the service enforces, pulled from config.
type Limits struct {
	MaxCiphertextBytes int64
	MaxRetention       time.Duration
	MaxViews           int
	ShortLinksEnabled  bool
}

type Service struct {
	store  store.Store
	pepper []byte
	limits Limits
}

func NewService(s store.Store, pepper []byte, limits Limits) *Service {
	return &Service{store: s, pepper: pepper, limits: limits}
}

type CreateInput struct {
	Ciphertext       []byte
	CryptoMetadata   []byte
	ExpiresInSeconds int64
	BurnAfterRead    bool
	MaxViews         *int
	OwnerToken       []byte // raw; we hash it with the pepper and never store it raw
	ShortLink        bool
}

type CreateResult struct {
	Slug      string
	ShortCode string // "" when no short link
	ExpiresAt time.Time
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	if int64(len(in.Ciphertext)) > s.limits.MaxCiphertextBytes || len(in.Ciphertext) == 0 {
		return nil, ErrTooLarge
	}
	if _, err := cryptoformat.ValidateMetadata(in.CryptoMetadata); err != nil {
		return nil, ErrBadMetadata
	}
	if len(in.OwnerToken) == 0 {
		return nil, ErrBadOwnerToken
	}

	d := time.Duration(in.ExpiresInSeconds) * time.Second
	if d < minRetention || d > s.limits.MaxRetention {
		return nil, ErrBadRetention
	}
	if in.MaxViews != nil {
		if *in.MaxViews < 1 || *in.MaxViews > s.limits.MaxViews {
			return nil, ErrBadMaxViews
		}
	}

	ownerHash := cryptoformat.HashCapability(s.pepper, in.OwnerToken)
	expiresAt := time.Now().UTC().Add(d)

	// Retry on the rare slug/short-code collision.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		params := store.CreatePasteParams{
			Slug:           cryptoformat.NewSlug(),
			Ciphertext:     in.Ciphertext,
			CryptoMetadata: in.CryptoMetadata,
			ExpiresAt:      expiresAt,
			BurnAfterRead:  in.BurnAfterRead,
			MaxViews:       in.MaxViews,
			OwnerTokenHash: ownerHash,
		}
		var code string
		if in.ShortLink && s.limits.ShortLinksEnabled {
			code = cryptoformat.NewShortCode()
			params.ShortCode = &code
		}
		p, err := s.store.CreatePaste(ctx, params)
		if err == nil {
			return &CreateResult{Slug: p.Slug, ShortCode: code, ExpiresAt: p.ExpiresAt}, nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

type Meta struct {
	Slug                   string
	RequiresExplicitReveal bool
	Passphrase             bool
	ExpiresAt              time.Time
	CryptoMetadata         []byte
}

func (s *Service) Meta(ctx context.Context, slug string) (*Meta, error) {
	m, err := s.store.GetPasteMeta(ctx, slug)
	if err != nil {
		return nil, mapUnavailable(err)
	}
	pass, _ := cryptoformat.ValidateMetadata(m.CryptoMetadata)
	return &Meta{
		Slug:                   m.Slug,
		RequiresExplicitReveal: m.RequiresExplicitReveal,
		Passphrase:             pass,
		ExpiresAt:              m.ExpiresAt,
		CryptoMetadata:         m.CryptoMetadata,
	}, nil
}

type Revealed struct {
	Ciphertext     []byte
	CryptoMetadata []byte
	ExpiresAt      time.Time
}

func (s *Service) Redeem(ctx context.Context, slug string) (*Revealed, error) {
	p, err := s.store.RedeemPaste(ctx, slug)
	if err != nil {
		return nil, mapUnavailable(err)
	}
	return &Revealed{Ciphertext: p.Ciphertext, CryptoMetadata: p.CryptoMetadata, ExpiresAt: p.ExpiresAt}, nil
}

// Status returns owner-visible detail, but only if the owner token checks out.
func (s *Service) Status(ctx context.Context, slug string, ownerToken []byte) (*store.Paste, error) {
	if len(ownerToken) == 0 {
		return nil, ErrForbidden
	}
	hash := cryptoformat.HashCapability(s.pepper, ownerToken)
	p, err := s.store.GetOwnerStatus(ctx, slug, hash)
	if err != nil {
		// Wrong token and missing paste look the same on purpose.
		return nil, ErrForbidden
	}
	return p, nil
}

func (s *Service) Delete(ctx context.Context, slug string, ownerToken []byte) error {
	if len(ownerToken) == 0 {
		return ErrForbidden
	}
	hash := cryptoformat.HashCapability(s.pepper, ownerToken)
	if err := s.store.RevokePaste(ctx, slug, hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Idempotent + non-leaky: nothing to delete reads as success.
			return nil
		}
		return err
	}
	return nil
}

// ResolveShort maps a short code to the internal path it points at ("/p/{slug}"
// or "/live/{slug}"). The client redirects there, keeping its fragment secret.
func (s *Service) ResolveShort(ctx context.Context, code string) (string, error) {
	path, err := s.store.ResolveShortCode(ctx, code)
	if err != nil {
		return "", mapUnavailable(err)
	}
	return path, nil
}

func mapUnavailable(err error) error {
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrExhausted) {
		return ErrUnavailable
	}
	return err
}
