package work

import "golang.org/x/sys/windows"

// longPath expands 8.3 short names ("RUNNER~1") in an existing path, so a
// session always records, and compares, one spelling of a folder. A path that
// cannot be expanded comes back as it was.
func longPath(p string) string {
	in, err := windows.UTF16FromString(p)
	if err != nil {
		return p
	}
	n, err := windows.GetLongPathName(&in[0], nil, 0)
	if err != nil || n == 0 {
		return p
	}
	out := make([]uint16, n)
	n, err = windows.GetLongPathName(&in[0], &out[0], n)
	if err != nil || n == 0 || int(n) > len(out) {
		return p
	}
	return windows.UTF16ToString(out[:n])
}
