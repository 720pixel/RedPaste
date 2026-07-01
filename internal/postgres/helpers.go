package postgres

import (
	"crypto/rand"
	"encoding/hex"
)

// random 128-bit hex id
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
