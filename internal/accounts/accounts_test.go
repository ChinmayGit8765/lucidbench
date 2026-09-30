package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secret = "SUPERSECRET-TOKEN-VALUE"

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func find(ps []Profile, provider, name string) *Profile {
	for i := range ps {
		if ps[i].Provider == provider && ps[i].Name == name {
			return &ps[i]
		}
	}
	return nil
}

func TestDetect(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour).UnixMilli()
	past := now.Add(-time.Hour).UnixMilli()
	cred := func(exp int64, refresh string) string {
		b, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{
			"accessToken": secret, "refreshToken": refresh, "expiresAt": exp}})
		return string(b)
	}
	tests := []struct {
		name     string
		setup    func(home, extra string) Roots
		provider string
		profile  string
		want     string
	}{
		{"claude logged in", func(h, e string) Roots {
			write(t, filepath.Join(h, ".claude", ".credentials.json"), cred(future, secret))
			return Roots{Home: h}
		}, "claude", "default", StatusLoggedIn},
		{"claude lapsed with refresh", func(h, e string) Roots {
			write(t, filepath.Join(h, ".claude", ".credentials.json"), cred(past, secret))
			return Roots{Home: h}
		}, "claude", "default", StatusLoggedIn},
		{"claude expired no refresh", func(h, e string) Roots {
			write(t, filepath.Join(h, ".claude", ".credentials.json"), cred(past, ""))
			return Roots{Home: h}
		}, "claude", "default", StatusExpired},
		{"claude missing", func(h, e string) Roots { return Roots{Home: h} }, "claude", "default", StatusMissing},
		{"claude extra dir", func(h, e string) Roots {
			write(t, filepath.Join(e, "work", ".credentials.json"), cred(future, secret))
			return Roots{Home: h, ExtraClaudeDirs: []string{filepath.Join(e, "work")}}
		}, "claude", "work", StatusLoggedIn},
		{"codex via CODEX_HOME", func(h, e string) Roots {
			write(t, filepath.Join(e, "codexhome", "auth.json"), `{"tokens":"`+secret+`"}`)
			return Roots{Home: h, CodexHome: filepath.Join(e, "codexhome")}
		}, "codex", "default", StatusLoggedIn},
		{"grok", func(h, e string) Roots {
			write(t, filepath.Join(h, ".grok", "auth.json"), `{"key":"`+secret+`"}`)
			return Roots{Home: h}
		}, "grok", "default", StatusLoggedIn},
		{"cursor unknown", func(h, e string) Roots {
			write(t, filepath.Join(h, ".cursor", "cli-config.json"), `{"version":1}`)
			return Roots{Home: h}
		}, "cursor", "default", StatusUnknown},
		{"cursor auth marker", func(h, e string) Roots {
			write(t, filepath.Join(h, ".cursor", "cli-config.json"), `{"authInfo":{"token":"`+secret+`"}}`)
			return Roots{Home: h}
		}, "cursor", "default", StatusLoggedIn},
		{"env api key", func(h, e string) Roots {
			return Roots{Home: h, Getenv: func(k string) string {
				if k == "ANTHROPIC_API_KEY" {
					return secret
				}
				return ""
			}}
		}, "claude", "ANTHROPIC_API_KEY", StatusLoggedIn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.setup(t.TempDir(), t.TempDir())
			r.Now = func() time.Time { return now }
			ps := Detect(r)
			p := find(ps, tc.provider, tc.profile)
			if p == nil {
				t.Fatalf("profile %s/%s not found in %+v", tc.provider, tc.profile, ps)
			}
			if p.Status != tc.want {
				t.Errorf("status = %s, want %s", p.Status, tc.want)
			}
			js, _ := json.Marshal(ps)
			if strings.Contains(string(js)+Table(ps), secret) {
				t.Error("secret value leaked into output")
			}
			if tc.name == "env api key" && p.Location != LocEnv {
				t.Errorf("location = %s, want env", p.Location)
			}
		})
	}
}

func TestParseVolumes(t *testing.T) {
	out := "lucidbench-claude-work\tlucidbench.profile=claude/work,other=x\n" +
		"lucidbench-grok-alt\tlucidbench.profile=grok/alt\r\n" +
		"unrelated\tfoo=bar\n" +
		"bad\tlucidbench.profile=noslash\n\n"
	ps := ParseVolumes(out)
	if len(ps) != 2 {
		t.Fatalf("got %d profiles: %+v", len(ps), ps)
	}
	if ps[0].Provider != "claude" || ps[0].Name != "work" || ps[0].Location != LocVolume || ps[0].ConfigDir != "lucidbench-claude-work" {
		t.Errorf("bad first profile: %+v", ps[0])
	}
	if ps[1].Provider != "grok" || ps[1].Name != "alt" {
		t.Errorf("bad second profile: %+v", ps[1])
	}
}

func TestProfileNameRE(t *testing.T) {
	for n, ok := range map[string]bool{"work": true, "a.b-c_1": true, "": false, "Bad": false, "-x": false, "a b": false, "a/b": false} {
		if ProfileNameRE.MatchString(n) != ok {
			t.Errorf("%q: want %v", n, ok)
		}
	}
}
