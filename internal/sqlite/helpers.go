package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// random 128-bit hex id
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// isUnique reports whether err is a SQLite UNIQUE-constraint failure.
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
