package cryptoformat

import (
	"crypto/rand"
	"encoding/base64"
)

// b64 is unpadded base64url, the compact, URL-safe encoding used for slugs,
// codes, and fragment secrets.
var b64 = base64.RawURLEncoding

// slug: 96 bits of entropy, 16 chars. Enough that enumeration is hopeless.
const slugBytes = 12

// short code: 48 bits, 8 chars. Kept short for tidy share links; codes map only
// to a RedPaste resource, expire with it, and misses return a generic 410, so a
// smaller space is fine here. Collisions are retried on create.
const shortCodeBytes = 6

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("cryptoformat: out of randomness: " + err.Error())
	}
	return b
}

func NewSlug() string      { return b64.EncodeToString(randBytes(slugBytes)) }
func NewShortCode() string { return b64.EncodeToString(randBytes(shortCodeBytes)) }

// DecodeToken decodes a base64url capability token from a client. Returns false
// on anything malformed so callers can reject it cleanly.
func DecodeToken(s string) ([]byte, bool) {
	b, err := b64.DecodeString(s)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}
