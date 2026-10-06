package remote

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// PairTTL is how long a pairing code can be exchanged.
const PairTTL = 5 * time.Minute

// MaxDeviceName is the longest device name kept.
const MaxDeviceName = 60

// seenEvery is how often last_seen is written back for a busy device.
const seenEvery = time.Minute

// Errors of the device store.
var (
	ErrBadCode       = errors.New("the pairing code is wrong, used or expired")
	ErrBadToken      = errors.New("unknown or revoked device token")
	ErrDeviceMissing = errors.New("no such device")
)

// Device is a paired phone. Only the token's SHA-256 is kept.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Hash     string    `json:"hash"` // hex SHA-256 of the token
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

// Public is a device without its hash, for the API.
type Public struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

func (d Device) Public() Public {
	return Public{ID: d.ID, Name: d.Name, Created: d.Created, LastSeen: d.LastSeen}
}

// Devices keeps the paired devices in devices.json and the one live pairing
// code in memory (as a hash, so a heap dump does not hold it either).
type Devices struct {
	Path string
	Now  func() time.Time

	mu       sync.Mutex
	loaded   bool
	list     []Device
	codeHash []byte
	codeExp  time.Time
}

func (d *Devices) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func hashOf(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return b
}

func (d *Devices) loadLocked() error {
	if d.loaded {
		return nil
	}
	b, err := os.ReadFile(d.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &d.list); err != nil {
			return err
		}
	}
	d.loaded = true
	return nil
}

func (d *Devices) saveLocked() error {
	list := d.list
	if list == nil {
		list = []Device{}
	}
	return saveJSON(d.Path, list)
}

// NewCode makes a one-time pairing code, valid for PairTTL. A new code
// replaces any earlier one. The code is 128 random bits in base32.
func (d *Devices) NewCode() (string, time.Time) {
	code := strings.TrimRight(base32.StdEncoding.EncodeToString(randomBytes(16)), "=")
	d.mu.Lock()
	defer d.mu.Unlock()
	d.codeHash = hashOf(code)
	d.codeExp = d.now().Add(PairTTL)
	return code, d.codeExp
}

// CancelCode drops the live pairing code.
func (d *Devices) CancelCode() {
	d.mu.Lock()
	d.codeHash, d.codeExp = nil, time.Time{}
	d.mu.Unlock()
}

// Pair exchanges a pairing code for a device token. The code works once and
// only before it expires. The token (32 random bytes, base64url) is
// returned here and never again; only its hash is stored.
func (d *Devices) Pair(code, name string) (token string, dev Device, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.loadLocked(); err != nil {
		return "", Device{}, err
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if d.codeHash == nil || code == "" || subtle.ConstantTimeCompare(hashOf(code), d.codeHash) != 1 {
		return "", Device{}, ErrBadCode
	}
	if !d.now().Before(d.codeExp) {
		d.codeHash, d.codeExp = nil, time.Time{}
		return "", Device{}, ErrBadCode
	}
	d.codeHash, d.codeExp = nil, time.Time{} // single use
	token = base64.RawURLEncoding.EncodeToString(randomBytes(32))
	now := d.now().UTC()
	dev = Device{ID: hex.EncodeToString(randomBytes(6)), Name: cleanName(name), Hash: hex.EncodeToString(hashOf(token)), Created: now, LastSeen: now}
	d.list = append(d.list, dev)
	if err := d.saveLocked(); err != nil {
		d.list = d.list[:len(d.list)-1]
		return "", Device{}, err
	}
	return token, dev, nil
}

func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if r := []rune(name); len(r) > MaxDeviceName {
		name = string(r[:MaxDeviceName])
	}
	if name == "" {
		name = "Phone"
	}
	return name
}

// Auth returns the device a token belongs to. Every stored hash is compared
// in constant time, so the time taken does not say which device was close.
func (d *Devices) Auth(token string) (Device, error) {
	if token == "" {
		return Device{}, ErrBadToken
	}
	want := []byte(hex.EncodeToString(hashOf(token)))
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.loadLocked(); err != nil {
		return Device{}, err
	}
	found := -1
	for i := range d.list {
		if subtle.ConstantTimeCompare(want, []byte(d.list[i].Hash)) == 1 {
			found = i
		}
	}
	if found < 0 {
		return Device{}, ErrBadToken
	}
	now := d.now().UTC()
	if now.Sub(d.list[found].LastSeen) >= seenEvery {
		d.list[found].LastSeen = now
		_ = d.saveLocked()
	}
	return d.list[found], nil
}

// Has reports whether a device is still paired (not revoked).
func (d *Devices) Has(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.loadLocked(); err != nil {
		return false
	}
	for _, dev := range d.list {
		if dev.ID == id {
			return true
		}
	}
	return false
}

// List returns the paired devices without their hashes.
func (d *Devices) List() ([]Public, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.loadLocked(); err != nil {
		return nil, err
	}
	out := make([]Public, 0, len(d.list))
	for _, dev := range d.list {
		out = append(out, dev.Public())
	}
	return out, nil
}

// Revoke forgets a device; its token stops working at once.
func (d *Devices) Revoke(id string) (Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.loadLocked(); err != nil {
		return Device{}, err
	}
	for i, dev := range d.list {
		if dev.ID != id {
			continue
		}
		old := d.list
		d.list = append(append([]Device{}, old[:i]...), old[i+1:]...)
		if err := d.saveLocked(); err != nil {
			d.list = old
			return Device{}, err
		}
		return dev, nil
	}
	return Device{}, ErrDeviceMissing
}
