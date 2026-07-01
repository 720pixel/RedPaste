package config

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePepperIsStable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rp.db")
	a := loadOrCreatePepper(p)
	b := loadOrCreatePepper(p)
	if len(a) < 32 {
		t.Fatalf("pepper too short: %d", len(a))
	}
	if string(a) != string(b) {
		t.Error("pepper should be stable across calls once persisted")
	}
}

func TestValidateRejectsTooLongRetention(t *testing.T) {
	c := &Config{
		Env:                 EnvDev,
		BaseURL:             "http://localhost:8080",
		MaxRetention:        200 * 24 * 60 * 60 * 1e9, // 200 days in ns, over the 180 cap
		LiveMaxParticipants: 8,
	}
	if err := c.validate(); err == nil {
		t.Error("expected retention over 180 days to be rejected")
	}
}

func TestValidateProdRequiresHTTPS(t *testing.T) {
	c := &Config{
		Env:                 EnvProd,
		BaseURL:             "http://insecure.example",
		ServerPepper:        make([]byte, 32),
		MaxRetention:        MaxRetention,
		LiveMaxParticipants: 8,
	}
	if err := c.validate(); err == nil {
		t.Error("production with an http base URL should be rejected")
	}
}
