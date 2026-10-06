package databases

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

func TestValidateRejectsLiteralPasswords(t *testing.T) {
	for _, pw := range []string{"PASSWORD-SENTINEL-2", "hunter2", "env:", "env:1BAD", "$DB_PASSWORD", "env:HAS SPACE", "${X}"} {
		_, err := Profile{ID: "a", Engine: Postgres, Host: "127.0.0.1", Password: config.SecretRef(pw)}.Validate()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("password %q: err = %v, want ErrInvalid", pw, err)
			continue
		}
		if !strings.HasPrefix(pw, "env:") && strings.Contains(err.Error(), pw) {
			t.Errorf("the error echoes the rejected password %q: %v", pw, err)
		}
	}
	p, err := Profile{ID: "ok", Engine: Redis, Host: "localhost", Password: "env:REDIS_PASSWORD"}.Validate()
	if err != nil || p.Password != "env:REDIS_PASSWORD" || p.Port != 6379 {
		t.Errorf("a reference is valid and the engine's port is the default: %+v, %v", p, err)
	}
	if _, err := (Profile{ID: "none", Engine: Redis, Host: "localhost"}).Validate(); err != nil {
		t.Errorf("no password at all is allowed: %v", err)
	}
}

func TestValidateRest(t *testing.T) {
	good := Profile{ID: "db1", Engine: Postgres, Host: "10.0.0.5", Port: 5432}
	for name, mut := range map[string]func(*Profile){
		"id":      func(p *Profile) { p.ID = "Has Space" },
		"id2":     func(p *Profile) { p.ID = "" },
		"engine":  func(p *Profile) { p.Engine = "oracle" },
		"host":    func(p *Profile) { p.Host = "a b" },
		"host2":   func(p *Profile) { p.Host = "" },
		"host3":   func(p *Profile) { p.Host = "x/y" },
		"port":    func(p *Profile) { p.Port = 70000 },
		"newline": func(p *Profile) { p.User = "a\nb" },
	} {
		p := good
		mut(&p)
		if _, err := p.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	p := good
	p.Prod, p.Readonly = true, false
	if v, err := p.Validate(); err != nil || !v.Readonly {
		t.Errorf("a prod connection is always read-only: %+v, %v", v, err)
	}
	if _, err := (Profile{ID: "v6", Engine: Postgres, Host: "[::1]"}).Validate(); err != nil {
		t.Errorf("an IPv6 literal is a host: %v", err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "sub", FileName)}
	if l, err := s.Load(); err != nil || len(l) != 0 {
		t.Fatalf("a missing file is an empty list: %v, %v", l, err)
	}
	if _, err := s.Add(Profile{ID: "zeta", Engine: Postgres, Host: "127.0.0.1", Port: 55432, Database: "d", User: "u", Password: "env:PG_PW", Label: "Zeta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Profile{ID: "alpha", Engine: Redis, Host: "localhost", Prod: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Profile{ID: "alpha", Engine: Redis, Host: "localhost"}); !errors.Is(err, ErrExists) {
		t.Errorf("a duplicate id: %v", err)
	}
	l, err := s.Load()
	if err != nil || len(l) != 2 || l[0].ID != "alpha" || !l[0].Readonly || l[1].Password != "env:PG_PW" {
		t.Fatalf("loaded %+v, %v", l, err)
	}
	raw, _ := os.ReadFile(s.Path)
	if !strings.Contains(string(raw), "password: env:PG_PW") {
		t.Errorf("the file keeps the reference:\n%s", raw)
	}
	if err := s.Remove("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("alpha"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v", err)
	}
	if _, err := s.Get("zeta"); err != nil {
		t.Error(err)
	}
}

func TestStoreRefusesAHandWrittenLiteralPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := "connections:\n  - id: a\n    engine: postgres\n    host: localhost\n    password: PASSWORD-SENTINEL-3\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (&Store{Path: path}).Load()
	if err == nil || !strings.Contains(err.Error(), `"a"`) || strings.Contains(err.Error(), "PASSWORD-SENTINEL-3") {
		t.Errorf("want an error naming the connection and not the value, got %v", err)
	}
}
