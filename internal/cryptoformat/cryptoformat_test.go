package cryptoformat

import (
	"bytes"
	"testing"
)

func TestValidateMetadata(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error
		wantPP  bool
	}{
		{"plain", `{"v":1,"alg":"AES-256-GCM","nonce":"n","salt":"s"}`, nil, false},
		{"passphrase", `{"v":1,"pass":{"on":true,"salt":"x","mem":65536}}`, nil, true},
		{"passphrase off", `{"v":1,"pass":{"on":false}}`, nil, false},
		{"bad version", `{"v":2}`, ErrUnsupportedVersion, false},
		{"malformed", `{not json`, ErrMetadataMalformed, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pp, err := ValidateMetadata([]byte(c.raw))
			if err != c.wantErr {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if pp != c.wantPP {
				t.Errorf("passphrase = %v, want %v", pp, c.wantPP)
			}
		})
	}
}

func TestMetadataTooLarge(t *testing.T) {
	big := append([]byte(`{"v":1,"x":"`), bytes.Repeat([]byte("a"), 5000)...)
	big = append(big, []byte(`"}`)...)
	if _, err := ValidateMetadata(big); err != ErrMetadataTooLarge {
		t.Errorf("err = %v, want ErrMetadataTooLarge", err)
	}
}

func TestCapabilityHashDeterministicAndCompares(t *testing.T) {
	pepper := []byte("pepper-pepper-pepper-pepper-1234")
	tok := []byte("some-owner-token")
	h1 := HashCapability(pepper, tok)
	h2 := HashCapability(pepper, tok)
	if !CapabilityMatches(h1, h2) {
		t.Error("same token should hash equal")
	}
	if CapabilityMatches(h1, HashCapability(pepper, []byte("other"))) {
		t.Error("different tokens must not match")
	}
	if CapabilityMatches(h1, HashCapability([]byte("different-pepper-abcdefghijklmno"), tok)) {
		t.Error("different pepper must not match")
	}
}

func TestIDLengths(t *testing.T) {
	if len(NewSlug()) != 16 {
		t.Errorf("slug len = %d, want 16", len(NewSlug()))
	}
	if len(NewShortCode()) != 8 {
		t.Errorf("short code len = %d, want 8", len(NewShortCode()))
	}
}
