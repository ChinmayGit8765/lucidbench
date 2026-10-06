// Package remote is the Phone remote extension: a second, deliberately small
// HTTP listener that a paired phone can reach over the LAN or a tailnet.
//
// lucidd itself keeps binding to 127.0.0.1. The remote listener is off by
// default; when the operator turns it on in Settings it binds to one chosen
// interface address (never 0.0.0.0 or ::) and serves only an allowlist: the
// Overview summary, the Work session list and tail, stopping a session, and
// viewing, approving or sending back a council brief. Everything else is 404.
//
// Files under <DataDir>/remote/:
//
//	settings.json   enabled, mode, address, port
//	devices.json    paired devices: id, name, sha256 of the token, created, last_seen
//	audit.jsonl     one line per pairing, revocation, setting change and remote write
package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// DefaultPort is the remote listener's port when none is set. lucidd's own
// port is 7420.
const DefaultPort = 7421

// Modes: lan binds the chosen interface address; tailnet binds this
// machine's tailscale address (100.64.0.0/10) when one is present.
const (
	ModeLAN     = "lan"
	ModeTailnet = "tailnet"
)

// Settings is <DataDir>/remote/settings.json.
type Settings struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`    // lan | tailnet
	Address string `json:"address"` // the interface address for mode lan
	Port    int    `json:"port"`
}

// DefaultSettings is what a data dir without settings.json gets: off, LAN
// mode, no address chosen.
func DefaultSettings() Settings {
	return Settings{Mode: ModeLAN, Port: DefaultPort}
}

var errBadSettings = errors.New("invalid remote settings")

// Validate checks the fields that do not depend on the machine's interfaces.
func (s Settings) Validate() error {
	if s.Mode != ModeLAN && s.Mode != ModeTailnet {
		return fmt.Errorf("%w: mode must be %q or %q", errBadSettings, ModeLAN, ModeTailnet)
	}
	if s.Port < 1024 || s.Port > 65535 {
		return fmt.Errorf("%w: port must be between 1024 and 65535", errBadSettings)
	}
	if s.Address != "" {
		ip := net.ParseIP(s.Address)
		if ip == nil {
			return fmt.Errorf("%w: %q is not an IP address", errBadSettings, s.Address)
		}
		if err := bindable(ip); err != nil {
			return fmt.Errorf("%w: %v", errBadSettings, err)
		}
	}
	return nil
}

// bindable refuses the addresses the remote must never listen on: the
// unspecified address (every interface) and multicast.
func bindable(ip net.IP) error {
	if ip.IsUnspecified() {
		return fmt.Errorf("%s listens on every interface; choose one interface", ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("%s is a multicast address", ip)
	}
	return nil
}

func loadSettings(path string) (Settings, error) {
	s := DefaultSettings()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSettings(), fmt.Errorf("%s: %w", path, err)
	}
	if s.Mode == "" {
		s.Mode = ModeLAN
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	return s, nil
}

// writeFileAtomic writes data to path through a temporary file in the same
// folder, readable by the owner only.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Chmod(name, 0o600)
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func saveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'))
}
