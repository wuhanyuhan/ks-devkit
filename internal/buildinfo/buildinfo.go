package buildinfo

import "strings"

var version = "dev"

// SetVersion records the CLI version injected by the main package.
func SetVersion(v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		v = "dev"
	}
	version = v
}

// Version returns the version reported by this CLI process.
func Version() string {
	return version
}
