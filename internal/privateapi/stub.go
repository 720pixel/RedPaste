//go:build !privateapi

package privateapi

import "context"

// Serve is a no-op in public builds. The private API is only compiled when the
// server is built with `-tags privateapi` and the (gitignored) implementation is
// present. This keeps the operator's automation surface out of the open-source
// binary entirely.
func Serve(_ context.Context, d Deps) error {
	d.Log.Warn("private API is enabled in config but this binary was built without it; rebuild with -tags privateapi")
	return nil
}
