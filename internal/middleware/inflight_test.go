package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestMaxInFlightShedsLoad(t *testing.T) {
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)

	h := MaxInFlight(2)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started.Done()
		<-release // hold the slot until the test lets go
		w.WriteHeader(http.StatusOK)
	}))

	// Occupy both slots.
	for i := 0; i < 2; i++ {
		go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	started.Wait()

	// Third request should be shed with 503.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("over-limit status = %d, want 503", rec.Code)
	}
	close(release)
}

func TestMaxInFlightBypassesWebSocket(t *testing.T) {
	// limit 0 -> defaults to 256, but force a full pool by using 1 and holding it
	release := make(chan struct{})
	var once sync.WaitGroup
	once.Add(1)
	h := MaxInFlight(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "" {
			once.Done()
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	once.Wait()

	// A websocket upgrade should still pass even though the pool is full.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/live/x/signal", nil)
	req.Header.Set("Upgrade", "websocket")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("websocket bypass status = %d, want 200", rec.Code)
	}
	close(release)
}
