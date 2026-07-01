package cryptoformat

import (
	"encoding/json"
	"errors"
)

// The server treats crypto metadata as an opaque blob it stores and hands back;
// it can't decrypt anything. We only peek at it enough to reject junk and to
// know whether a passphrase is in play (which forces explicit reveal).

const maxMetadataBytes = 4096

// SupportedVersion is the only envelope version we accept for now. Unknown
// versions are rejected rather than reinterpreted.
const SupportedVersion = 1

var (
	ErrMetadataTooLarge   = errors.New("crypto metadata too large")
	ErrMetadataMalformed  = errors.New("crypto metadata malformed")
	ErrUnsupportedVersion = errors.New("unsupported crypto version")
)

// metaPeek is just the fields the server cares about; everything else in the
// blob is preserved untouched.
type metaPeek struct {
	Version int `json:"v"`
	Pass    *struct {
		On bool `json:"on"`
	} `json:"pass"`
}

// ValidateMetadata checks size, JSON validity, and version, and reports whether
// the paste is passphrase-protected.
func ValidateMetadata(raw []byte) (passphrase bool, err error) {
	if len(raw) > maxMetadataBytes {
		return false, ErrMetadataTooLarge
	}
	var m metaPeek
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, ErrMetadataMalformed
	}
	if m.Version != SupportedVersion {
		return false, ErrUnsupportedVersion
	}
	return m.Pass != nil && m.Pass.On, nil
}
