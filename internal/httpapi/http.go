package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

func wantsJSON(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return ct == "" || strings.HasPrefix(ct, "application/json")
}

func bearerToken(r *http.Request) ([]byte, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return nil, false
	}
	tok, err := b64.DecodeString(strings.TrimSpace(h[len(p):]))
	if err != nil || len(tok) == 0 {
		return nil, false
	}
	return tok, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
