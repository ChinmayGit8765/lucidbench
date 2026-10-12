package update

import (
	"strconv"
	"strings"
)

// Semver is a parsed MAJOR.MINOR.PATCH[-PRERELEASE] version. Build metadata
// (+...) is dropped: it never changes precedence.
type Semver struct {
	Major, Minor, Patch int
	Pre                 string
}

func (v Semver) String() string {
	s := strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Parse reads "1.2.3", "v1.2.3", "1.2.3-beta.4" and "1.2.3+build". Anything
// else, "dev" included, is not a version.
func Parse(s string) (Semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 || (hasPre && pre == "") {
		return Semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p == "" || (len(p) > 1 && p[0] == '0') {
			return Semver{}, false
		}
		n[i] = v
	}
	if hasPre {
		for _, id := range strings.Split(pre, ".") {
			if id == "" {
				return Semver{}, false
			}
			for _, r := range id {
				if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
					return Semver{}, false
				}
			}
		}
	}
	return Semver{n[0], n[1], n[2], pre}, true
}

// Compare returns -1, 0 or 1 by semver precedence: a release is newer than
// its prereleases, numeric identifiers compare as numbers (beta.10 is newer
// than beta.9), and a longer prerelease wins when the shared part is equal.
func Compare(a, b Semver) int {
	for _, d := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			return sign(d[0] - d[1])
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	as, bs := strings.Split(a.Pre, "."), strings.Split(b.Pre, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, xerr := strconv.Atoi(as[i])
		y, yerr := strconv.Atoi(bs[i])
		switch {
		case xerr == nil && yerr == nil:
			if x != y {
				return sign(x - y)
			}
		case xerr == nil:
			return -1 // numeric identifiers sort before alphanumeric ones
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(as) - len(bs))
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
