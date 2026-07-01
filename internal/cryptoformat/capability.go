// Package cryptoformat covers the server's slice of the crypto: hashing
// capability tokens, minting identifiers, and sanity-checking the opaque crypto
// metadata clients send. It never touches plaintext or content keys; those
// never reach the server.
package cryptoformat

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
)

// HashCapability keys an HMAC with the deployment pepper. Capability tokens are
// already high-entropy random secrets, so a plain keyed hash is enough; no need
// for a slow password hash. The raw token is never stored.
func HashCapability(pepper, token []byte) []byte {
	m := hmac.New(sha256.New, pepper)
	m.Write(token)
	return m.Sum(nil)
}

// CapabilityMatches compares two capability hashes in constant time.
func CapabilityMatches(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
