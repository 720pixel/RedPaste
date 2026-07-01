package ratelimit

import (
	"net/http"
	"testing"
	"time"
)

func TestBucketBurstThenRefill(t *testing.T) {
	l := New(Rule{Every: time.Second, Burst: 3})
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	// burst of 3 allowed, 4th denied
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("token %d should be allowed", i)
		}
	}
	if l.Allow("k") {
		t.Fatal("4th request should be denied")
	}
	// after 2s, 2 tokens back
	now = now.Add(2 * time.Second)
	if !l.Allow("k") || !l.Allow("k") {
		t.Fatal("2 refilled tokens should be allowed")
	}
	if l.Allow("k") {
		t.Fatal("only 2 should have refilled")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(Rule{Every: time.Minute, Burst: 1})
	if !l.Allow("a") {
		t.Fatal("a first should pass")
	}
	if !l.Allow("b") {
		t.Fatal("b is a separate bucket and should pass")
	}
	if l.Allow("a") {
		t.Fatal("a is now empty")
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	m := NewManager([]string{"10.0.0.0/8"})

	// untrusted direct peer: use RemoteAddr, ignore spoofed XFF
	r := &http.Request{RemoteAddr: "203.0.113.5:1234", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := m.ClientIP(r); got != "203.0.113.5" {
		t.Errorf("untrusted peer ip = %q, want 203.0.113.5 (ignore XFF)", got)
	}

	// trusted proxy: take rightmost non-proxy from XFF
	r2 := &http.Request{RemoteAddr: "10.1.2.3:5678", Header: http.Header{}}
	r2.Header.Set("X-Forwarded-For", "198.51.100.9, 10.9.9.9")
	if got := m.ClientIP(r2); got != "198.51.100.9" {
		t.Errorf("trusted proxy ip = %q, want 198.51.100.9", got)
	}
}

func TestManagerUnknownCategoryAllows(t *testing.T) {
	m := NewManager(nil)
	if !m.Allow("nope", "1.1.1.1") {
		t.Error("unregistered category should be allowed")
	}
}
