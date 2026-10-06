package remote

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Register adds the desktop's routes for the remote to mux (lucidd's own
// 127.0.0.1 listener, never the remote one):
//
//	GET    /api/remote                 status: settings, listening, url, error, tailnet address
//	PUT    /api/remote/settings        {enabled, mode, address, port}; binds or unbinds at once
//	GET    /api/remote/interfaces      the addresses the picker offers
//	POST   /api/remote/pair            a one-time code (5 minutes), its URL and QR code (SVG)
//	DELETE /api/remote/pair            cancel the live code
//	GET    /api/remote/devices         paired devices, without token hashes
//	DELETE /api/remote/devices/{id}    revoke a device
//	GET    /api/remote/audit?limit=    recent remote actions, newest first
//
// Every write needs X-Lucid-Confirm.
func Register(mux *http.ServeMux, m *Manager) {
	mux.HandleFunc("GET /api/remote", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, m.Status())
	})
	mux.HandleFunc("PUT /api/remote/settings", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var s Settings
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		was := m.Status().Settings.Enabled
		st, err := m.Apply(s)
		if errors.Is(err, errBadSettings) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if was != s.Enabled {
			action := "disable"
			if s.Enabled {
				action = "enable"
			}
			result := "ok"
			if err != nil {
				result = err.Error()
			}
			m.Audit.Add(AuditEntry{Action: action, Target: st.Address, Result: result})
		}
		if !s.Enabled {
			m.Devices.CancelCode()
		}
		// A bind error is part of the status, not a failed save.
		apiutil.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /api/remote/interfaces", func(w http.ResponseWriter, r *http.Request) {
		ifs, err := m.interfaces()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if ifs == nil {
			ifs = []Interface{}
		}
		apiutil.WriteJSON(w, http.StatusOK, ifs)
	})
	mux.HandleFunc("POST /api/remote/pair", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		st := m.Status()
		if !st.Listening {
			http.Error(w, "turn the remote on first", http.StatusConflict)
			return
		}
		code, exp := m.Devices.NewCode()
		url := "http://" + st.Address + "/r#pair=" + code
		svg, err := QRSVG(url)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"code": code, "url": url, "expires": exp.UTC().Format(time.RFC3339), "qr_svg": svg})
	})
	mux.HandleFunc("DELETE /api/remote/pair", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		m.Devices.CancelCode()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/remote/devices", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.Devices.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("DELETE /api/remote/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		dev, err := m.Devices.Revoke(r.PathValue("id"))
		if errors.Is(err, ErrDeviceMissing) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		m.Audit.Add(AuditEntry{Action: "revoke", Device: dev.ID, Name: dev.Name, Result: "ok"})
		apiutil.WriteJSON(w, http.StatusOK, dev.Public())
	})
	mux.HandleFunc("GET /api/remote/audit", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if n <= 0 || n > 500 {
			n = 50
		}
		list, err := m.Audit.Recent(n)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, list)
	})
}
