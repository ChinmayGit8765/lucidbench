package config

import (
	"strings"
	"testing"
)

func TestParseSecretRefAccepts(t *testing.T) {
	for _, s := range []string{"env:MY_KEY", "env:_x", "env:Token2"} {
		ref, err := ParseSecretRef(s)
		if err != nil {
			t.Errorf("%q rejected: %v", s, err)
			continue
		}
		if ref.Env() != strings.TrimPrefix(s, "env:") {
			t.Errorf("%q: Env() = %q", s, ref.Env())
		}
	}
}

func TestParseSecretRefRejectsLiterals(t *testing.T) {
	for _, s := range []string{
		"sk-ant-api03-abcdefghijklmnop",
		"sk-proj-1234567890abcdef",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"AKIAIOSFODNN7EXAMPLE",
		"xai-abcdefghijklmnop",
		"hunter2",
		"",
		"env:",
		"env:has space",
		"env:sk-ant-abc",
		"ENV:NAME",
	} {
		_, err := ParseSecretRef(s)
		if err == nil {
			t.Errorf("%q accepted, want rejection", s)
			continue
		}
		if len(s) >= 8 && strings.Contains(err.Error(), s) {
			t.Errorf("error echoes the rejected value %q: %v", s, err)
		}
	}
}

func TestSecretRefResolve(t *testing.T) {
	ref, _ := ParseSecretRef("env:LUCID_TEST_KEY")
	got := ref.Resolve(func(k string) string {
		if k == "LUCID_TEST_KEY" {
			return "v"
		}
		return ""
	})
	if got != "v" {
		t.Fatalf("Resolve = %q", got)
	}
}
