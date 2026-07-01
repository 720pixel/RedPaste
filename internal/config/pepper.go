package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

func randomPepper() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("config: unable to read random bytes for pepper: " + err.Error())
	}
	return b
}

// devEphemeralPepper hands back a random 32-byte pepper that rotates every
// restart. Used only when we can't persist one.
func devEphemeralPepper() []byte {
	return randomPepper()
}

// loadOrCreatePepper keeps a stable pepper next to the SQLite database so a
// zero-config `docker run` (or `make dev`) survives restarts without the user
// setting anything. Falls back to an ephemeral pepper if the file can't be used.
func loadOrCreatePepper(sqlitePath string) []byte {
	dir := filepath.Dir(sqlitePath)
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, ".redpaste-pepper")

	if raw, err := os.ReadFile(path); err == nil {
		if p, err := hex.DecodeString(strings.TrimSpace(string(raw))); err == nil && len(p) >= 32 {
			return p
		}
	}

	p := randomPepper()
	// 0600 so other users on the host can't read it. Best-effort: if the write
	// fails we still run with this in-memory pepper for the session.
	_ = os.WriteFile(path, []byte(hex.EncodeToString(p)), 0o600)
	return p
}
