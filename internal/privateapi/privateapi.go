// Package privateapi is the operator's private automation API. It is disabled by
// default and deliberately kept out of the public repository: the real
// implementation lives in build-tagged files that are gitignored, and only this
// shared shape plus a no-op stub are published. See docs/private-api for the
// operator documentation (also private).
//
// The public build (no `privateapi` tag) links the stub, so the server compiles
// and runs with the private API simply absent.
package privateapi

import (
	"log/slog"

	"github.com/720pRed/redpaste/internal/paste"
	"github.com/720pRed/redpaste/internal/store"
)

// Deps is everything the private listener needs. Defined here (untagged) so the
// server entrypoint can construct it regardless of which build is compiled.
type Deps struct {
	PasteSvc     *paste.Service
	Store        store.Store // optional: lets tokens live in a database table (checked live) alongside the flat file
	Pepper       []byte
	Listen       string
	AllowedCIDRs []string
	RequireMTLS  bool
	TokensFile   string
	BaseURL      string
	ShortBase    string
	Production   bool
	DevInsecure  bool
	Log          *slog.Logger
}
