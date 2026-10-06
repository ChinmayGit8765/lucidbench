//go:build !windows

package work

// longPath only matters on Windows, where a path can have a short 8.3 form.
func longPath(p string) string { return p }
