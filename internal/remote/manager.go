package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Manager owns the remote listener: it reads settings.json, binds when the
// remote is enabled, and rebinds when the operator changes the settings.
// Building one binds nothing; only Start and Apply do, and only when the
// settings say enabled.
type Manager struct {
	Dir     string // <DataDir>/remote
	Devices *Devices
	Audit   *Audit
	// Interfaces lists the bindable addresses; nil asks the OS.
	Interfaces func() ([]Interface, error)
	// Listen opens the listener; nil is net.Listen. Tests count binds here.
	Listen func(network, addr string) (net.Listener, error)
	// Assets is the built phone page, set before Start.
	Assets fs.FS

	mu       sync.Mutex
	services Services
	surface  *Surface
	settings Settings
	srv      *http.Server
	bound    string // host:port while listening
	lastErr  string
}

// NewManager returns a manager keeping its files in dir.
func NewManager(dir string) *Manager {
	return &Manager{
		Dir:     dir,
		Devices: &Devices{Path: filepath.Join(dir, "devices.json")},
		Audit:   &Audit{Path: filepath.Join(dir, "audit.jsonl")},
	}
}

// SetServices gives the remote the services it reads and acts on. lucidd's
// handler calls it once it has built them.
func (m *Manager) SetServices(s Services) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services = s
	m.surface = nil
}

func (m *Manager) settingsPath() string { return filepath.Join(m.Dir, "settings.json") }

func (m *Manager) interfaces() ([]Interface, error) {
	if m.Interfaces != nil {
		return m.Interfaces()
	}
	return SystemInterfaces()
}

// Status is GET /api/remote.
type Status struct {
	Settings  Settings `json:"settings"`
	Listening bool     `json:"listening"`
	Address   string   `json:"address,omitempty"` // host:port while listening
	URL       string   `json:"url,omitempty"`     // the phone page while listening
	Error     string   `json:"error,omitempty"`   // why it is not listening although enabled
	Tailnet   string   `json:"tailnet,omitempty"` // this machine's tailscale address, when present
}

// Status reports the settings and whether the listener is up.
func (m *Manager) Status() Status {
	m.mu.Lock()
	st := Status{Settings: m.settings, Listening: m.srv != nil, Address: m.bound, Error: m.lastErr}
	m.mu.Unlock()
	if st.Listening {
		st.URL = "http://" + st.Address + "/r/"
	}
	if ifs, err := m.interfaces(); err == nil {
		for _, i := range ifs {
			if i.Kind == "tailnet" {
				st.Tailnet = i.Address
				break
			}
		}
	}
	return st
}

// Start reads settings.json and binds when the remote is enabled. A data dir
// without settings.json leaves the remote off.
func (m *Manager) Start() error {
	s, err := loadSettings(m.settingsPath())
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = s
	if err != nil {
		m.lastErr = err.Error()
		return err
	}
	return m.bindLocked()
}

// Apply validates and saves new settings, then stops the listener and binds
// again when enabled.
func (m *Manager) Apply(s Settings) (Status, error) {
	if s.Mode == "" {
		s.Mode = ModeLAN
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if err := s.Validate(); err != nil {
		return m.Status(), err
	}
	m.mu.Lock()
	if err := saveJSON(m.settingsPath(), s); err != nil {
		m.mu.Unlock()
		return m.Status(), err
	}
	m.settings = s
	err := m.bindLocked()
	m.mu.Unlock()
	return m.Status(), err
}

// Close stops the listener.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if m.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = m.srv.Shutdown(ctx)
	cancel()
	_ = m.srv.Close() // ends open event streams
	m.srv, m.bound = nil, ""
}

// address picks the host to bind for the settings, or says why there is none.
func (m *Manager) addressLocked() (string, error) {
	ifs, err := m.interfaces()
	if err != nil {
		return "", fmt.Errorf("cannot list the network interfaces: %v", err)
	}
	if m.settings.Mode == ModeTailnet {
		for _, i := range ifs {
			if i.Kind == "tailnet" {
				return i.Address, nil
			}
		}
		return "", errors.New("no tailscale address (100.64.0.0/10) on this machine; start Tailscale or choose LAN mode")
	}
	if m.settings.Address == "" {
		return "", errors.New("choose the interface the remote listens on")
	}
	for _, i := range ifs {
		if i.Address == m.settings.Address {
			return i.Address, nil
		}
	}
	return "", fmt.Errorf("%s is not an address of this machine now", m.settings.Address)
}

func (m *Manager) bindLocked() error {
	m.stopLocked()
	m.lastErr = ""
	if !m.settings.Enabled {
		return nil
	}
	host, err := m.addressLocked()
	if err == nil {
		err = bindable(net.ParseIP(host))
	}
	if err != nil {
		m.lastErr = err.Error()
		return err
	}
	listen := m.Listen
	if listen == nil {
		listen = net.Listen
	}
	addr := net.JoinHostPort(host, strconv.Itoa(m.settings.Port))
	ln, err := listen("tcp", addr)
	if err != nil {
		m.lastErr = err.Error()
		return err
	}
	if m.surface == nil {
		m.surface = NewSurface(m.services, m.Devices, m.Audit, m.Assets)
	}
	srv := &http.Server{
		Handler:           m.surface.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	m.srv, m.bound = srv, ln.Addr().String()
	log.Printf("remote: listening on %s", m.bound)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("remote: %v", err)
		}
	}()
	return nil
}
