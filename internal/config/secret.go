package config

import (
	"fmt"
	"regexp"
)

// SecretRef is a reference to a secret, never the secret itself. The only
// accepted form is "env:NAME", naming an environment variable that holds the
// value. Literal values are rejected so a key can never be committed or
// written to a config file by accident.
type SecretRef string

var secretRefRE = regexp.MustCompile(`^env:[A-Za-z_][A-Za-z0-9_]*$`)

// ParseSecretRef validates s as a secret reference. The error never echoes s,
// because s may be a literal secret that was pasted by mistake.
func ParseSecretRef(s string) (SecretRef, error) {
	if !secretRefRE.MatchString(s) {
		return "", fmt.Errorf("secrets must be references of the form env:NAME, never literal values")
	}
	return SecretRef(s), nil
}

// Env returns the environment variable name the reference points at.
func (s SecretRef) Env() string { return string(s)[len("env:"):] }

// Resolve looks the referenced value up. It is the only place a secret value
// is read; callers must not log or print the result.
func (s SecretRef) Resolve(getenv func(string) string) string { return getenv(s.Env()) }

// String returns the reference, which is safe to print.
func (s SecretRef) String() string { return string(s) }
