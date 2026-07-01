// Package httpapi is the browser-facing REST layer over the paste service.
// Everything in and out is ciphertext or opaque metadata: no plaintext, ever.
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/720pRed/redpaste/internal/paste"
	"github.com/720pRed/redpaste/internal/store"
	"github.com/go-chi/chi/v5"
)

var b64 = base64.RawURLEncoding

// Metrics is the tiny slice of counters this layer bumps. Nil is fine.
type Metrics interface {
	IncCreated()
	IncRedeemed()
}

type Handler struct {
	svc       *paste.Service
	baseURL   string
	shortBase string
	metrics   Metrics
}

func NewHandler(svc *paste.Service, baseURL, shortBase string, metrics Metrics) *Handler {
	return &Handler{svc: svc, baseURL: strings.TrimRight(baseURL, "/"), shortBase: strings.TrimRight(shortBase, "/"), metrics: metrics}
}

// Mount hangs the paste routes off the given router (expected at /api/v1).
func (h *Handler) Mount(r chi.Router) {
	r.Post("/pastes", h.create)
	r.Get("/pastes/{slug}", h.meta)
	r.Post("/pastes/{slug}/redeem", h.redeem)
	r.Get("/pastes/{slug}/status", h.status)
	r.Delete("/pastes/{slug}", h.delete)
	r.Get("/shortlinks/{code}", h.resolveShort)
}

type createRequest struct {
	Ciphertext     string          `json:"ciphertext"`
	CryptoMetadata json.RawMessage `json:"cryptoMetadata"`
	Policy         struct {
		ExpiresInSeconds int64 `json:"expiresInSeconds"`
		BurnAfterRead    bool  `json:"burnAfterRead"`
		MaxViews         *int  `json:"maxViews"`
	} `json:"policy"`
	OwnerToken string `json:"ownerToken"`
	ShortLink  bool   `json:"shortLink"`
}

type createResponse struct {
	Slug         string    `json:"slug"`
	URLBase      string    `json:"urlBase"`
	ShortURLBase *string   `json:"shortUrlBase"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !wantsJSON(r) {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	ct, err := b64.DecodeString(req.Ciphertext)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_ciphertext")
		return
	}
	ownerTok, err := b64.DecodeString(req.OwnerToken)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_owner_token")
		return
	}

	res, err := h.svc.Create(r.Context(), paste.CreateInput{
		Ciphertext:       ct,
		CryptoMetadata:   req.CryptoMetadata,
		ExpiresInSeconds: req.Policy.ExpiresInSeconds,
		BurnAfterRead:    req.Policy.BurnAfterRead,
		MaxViews:         req.Policy.MaxViews,
		OwnerToken:       ownerTok,
		ShortLink:        req.ShortLink,
	})
	if err != nil {
		writeCreateErr(w, err)
		return
	}
	if h.metrics != nil {
		h.metrics.IncCreated()
	}

	resp := createResponse{
		Slug:      res.Slug,
		URLBase:   h.baseURL + "/p/" + res.Slug,
		ExpiresAt: res.ExpiresAt,
	}
	if res.ShortCode != "" {
		s := h.shortBase + "/s/" + res.ShortCode
		resp.ShortURLBase = &s
	}
	writeJSON(w, http.StatusCreated, resp)
}

type metaResponse struct {
	Slug                   string          `json:"slug"`
	Available              bool            `json:"available"`
	RequiresExplicitReveal bool            `json:"requiresExplicitReveal"`
	PassphraseProtected    bool            `json:"passphraseProtected"`
	ExpiresAt              time.Time       `json:"expiresAt"`
	CryptoMetadata         json.RawMessage `json:"crypto"`
}

func (h *Handler) meta(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Meta(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		writeUnavailable(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metaResponse{
		Slug:                   m.Slug,
		Available:              true,
		RequiresExplicitReveal: m.RequiresExplicitReveal,
		PassphraseProtected:    m.Passphrase,
		ExpiresAt:              m.ExpiresAt,
		CryptoMetadata:         m.CryptoMetadata,
	})
}

type redeemResponse struct {
	Ciphertext     string          `json:"ciphertext"`
	CryptoMetadata json.RawMessage `json:"crypto"`
	ExpiresAt      time.Time       `json:"expiresAt"`
}

func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	rev, err := h.svc.Redeem(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		writeUnavailable(w, err)
		return
	}
	if h.metrics != nil {
		h.metrics.IncRedeemed()
	}
	writeJSON(w, http.StatusOK, redeemResponse{
		Ciphertext:     b64.EncodeToString(rev.Ciphertext),
		CryptoMetadata: rev.CryptoMetadata,
		ExpiresAt:      rev.ExpiresAt,
	})
}

type statusResponse struct {
	Slug            string     `json:"slug"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	BurnAfterRead   bool       `json:"burnAfterRead"`
	MaxViews        *int       `json:"maxViews"`
	SuccessfulViews int        `json:"successfulViews"`
	LastViewedAt    *time.Time `json:"lastViewedAt"`
	State           string     `json:"state"`
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, err := h.svc.Status(r.Context(), chi.URLParam(r, "slug"), tok)
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Slug:            p.Slug,
		CreatedAt:       p.CreatedAt,
		ExpiresAt:       p.ExpiresAt,
		BurnAfterRead:   p.BurnAfterRead,
		MaxViews:        p.MaxViews,
		SuccessfulViews: p.SuccessfulViews,
		LastViewedAt:    p.LastViewedAt,
		State:           pasteState(p),
	})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "slug"), tok); err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resolveShort(w http.ResponseWriter, r *http.Request) {
	path, err := h.svc.ResolveShort(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeUnavailable(w, err)
		return
	}
	// Already the internal path ("/p/{slug}" or "/live/{slug}"). The client does
	// location.replace(path + '#...') so the fragment secret is preserved and never sent here.
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func pasteState(p *store.Paste) string {
	switch {
	case p.RevokedAt != nil:
		return "revoked"
	case p.ExhaustedAt != nil:
		return "exhausted"
	case time.Now().After(p.ExpiresAt):
		return "expired"
	default:
		return "active"
	}
}

func writeCreateErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, paste.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large")
	case errors.Is(err, paste.ErrBadRetention):
		writeErr(w, http.StatusBadRequest, "bad_retention")
	case errors.Is(err, paste.ErrBadMaxViews):
		writeErr(w, http.StatusBadRequest, "bad_max_views")
	case errors.Is(err, paste.ErrBadMetadata):
		writeErr(w, http.StatusBadRequest, "bad_metadata")
	case errors.Is(err, paste.ErrBadOwnerToken):
		writeErr(w, http.StatusBadRequest, "bad_owner_token")
	default:
		writeErr(w, http.StatusInternalServerError, "internal_error")
	}
}

// Expired/revoked/missing all collapse to a generic 410 so we never reveal which.
func writeUnavailable(w http.ResponseWriter, err error) {
	if errors.Is(err, paste.ErrUnavailable) {
		writeErr(w, http.StatusGone, "unavailable")
		return
	}
	writeErr(w, http.StatusInternalServerError, "internal_error")
}
