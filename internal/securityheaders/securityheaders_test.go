package securityheaders

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testHandler() http.Handler {
	return Base(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestBaseHeadersAlwaysPresent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	testHandler().ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
}

func TestSecretRoutesGetNoindex(t *testing.T) {
	secret := []string{"/p/abc", "/live/xyz", "/s/code", "/manage/slug", "/api/v1/pastes"}
	for _, path := range secret {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		testHandler().ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Robots-Tag"); got != robotsSecret {
			t.Errorf("%s: X-Robots-Tag = %q, want %q", path, got, robotsSecret)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store, private, max-age=0" {
			t.Errorf("%s: Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestPublicRoutesAreIndexable(t *testing.T) {
	public := []string{"/", "/security", "/privacy"}
	for _, path := range public {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		testHandler().ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Robots-Tag"); got != "" {
			t.Errorf("%s: unexpected X-Robots-Tag %q on indexable route", path, got)
		}
	}
}
