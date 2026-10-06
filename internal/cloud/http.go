package cloud

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// DeployStatus is one projects.yaml deploy entry, matched against the
// inventory.
type DeployStatus struct {
	Provider    string `json:"provider"`
	Service     string `json:"service"`
	Region      string `json:"region,omitempty"`
	Project     string `json:"project,omitempty"`
	Matched     bool   `json:"matched"`
	Status      string `json:"status,omitempty"`
	Detail      string `json:"detail,omitempty"`
	URL         string `json:"url,omitempty"`
	Console     string `json:"console,omitempty"`
	Updated     string `json:"updated,omitempty"`
	UpdatedText string `json:"updated_text,omitempty"`
	// Note says why an entry did not match.
	Note string `json:"note,omitempty"`
}

// match finds a deploy entry in one provider's inventory.
func match(d projects.Deploy, sum Summary) DeployStatus {
	out := DeployStatus{Provider: d.Provider, Service: d.Service, Region: d.Region, Project: d.Project}
	if sum.State != StateConnected {
		out.Note = sum.Label + ": " + firstOf(sum.Message, sum.State)
		return out
	}
	wantProject := firstOf(d.Project, sum.Scope)
	for _, sec := range sum.Sections {
		for _, r := range sec.Resources {
			if r.Name != d.Service || r.Kind == KindDeployment || r.Kind == KindGCPProject {
				continue
			}
			if d.Provider == "gcloud" && ((d.Region != "" && r.Region != d.Region) || r.Project != wantProject) {
				continue
			}
			out.Matched, out.Status, out.Detail, out.URL, out.Console = true, firstOf(r.Status, StatusUnknown), r.Detail, r.URL, r.Console
			out.Updated, out.UpdatedText = r.Updated, r.UpdatedText
			return out
		}
	}
	out.Note = "not found in " + sum.Label
	return out
}

// Deploys matches every project's deploy entries to the inventory, reading
// only the providers they name.
func (s *Service) Deploys(ctx context.Context, force bool) map[string][]DeployStatus {
	out := map[string][]DeployStatus{}
	if s.Projects == nil {
		return out
	}
	list := s.Projects()
	sums := map[string]Summary{}
	for _, p := range list {
		for _, d := range p.Deploy {
			if _, ok := sums[d.Provider]; !ok {
				sums[d.Provider], _ = s.Get(ctx, d.Provider, force)
			}
		}
	}
	for _, p := range list {
		for _, d := range p.Deploy {
			out[p.ID] = append(out[p.ID], match(d, sums[d.Provider]))
		}
	}
	return out
}

// Register adds the /api/cloud routes to mux:
//
//	GET /api/cloud                     every provider's inventory
//	GET /api/cloud/{provider}          one provider (gcloud, wrangler, vercel, az, aws)
//	GET /api/cloud/deploys             projects.yaml deploy entries matched to the inventory
//
// Results are cached for five minutes; ?refresh=1 runs the CLIs again. All
// routes only read: they run each CLI's list and identity commands.
func Register(mux *http.ServeMux, s *Service) {
	read := func(r *http.Request) (context.Context, context.CancelFunc, bool) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		return ctx, cancel, r.URL.Query().Get("refresh") == "1"
	}
	mux.HandleFunc("GET /api/cloud", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel, force := read(r)
		defer cancel()
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"providers": s.All(ctx, force)})
	})
	mux.HandleFunc("GET /api/cloud/deploys", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel, force := read(r)
		defer cancel()
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"projects": s.Deploys(ctx, force)})
	})
	mux.HandleFunc("GET /api/cloud/{provider}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("provider")
		if !slices.Contains(ProviderIDs(), id) {
			http.Error(w, "unknown provider "+id, http.StatusNotFound)
			return
		}
		ctx, cancel, force := read(r)
		defer cancel()
		sum, _ := s.Get(ctx, id, force)
		apiutil.WriteJSON(w, http.StatusOK, sum)
	})
}
