// Package version reports the omca build identity.
package version

// Version is overridden at build time via -ldflags "-X ... =<tag>".
var Version = "dev"

// String returns the human-readable version string.
func String() string {
	return "omca " + Version
}
