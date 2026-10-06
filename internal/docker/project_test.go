package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestActProject(t *testing.T) {
	ctx := context.Background()
	p := policy
	p.Projects = append(p.Projects, "shop")
	f := &fake{}
	done, err := ActProject(ctx, f.run, p, "shop", "stop")
	if err != nil || !slices.Equal(done, []string{"shop-db-1"}) || len(f.acted()) != 1 {
		t.Fatalf("stop shop: %v %v %v", done, err, f.acted())
	}
	// Already running: nothing to start.
	f = &fake{}
	if done, err := ActProject(ctx, f.run, p, "shop", "start"); err != nil || len(done) != 0 || len(f.acted()) != 0 {
		t.Fatalf("start running shop: %v %v %v", done, err, f.acted())
	}
	for _, c := range []struct {
		project, action string
		want            error
	}{
		{"shop", "stop", ErrRefused}, // with the default policy
		{"nope", "start", ErrNotFound},
		{"../x", "start", ErrNotFound},
	} {
		f := &fake{}
		if _, err := ActProject(ctx, f.run, policy, c.project, c.action); !errors.Is(err, c.want) {
			t.Errorf("ActProject(%s, %s) = %v, want %v", c.project, c.action, err, c.want)
		}
		if len(f.acted()) != 0 {
			t.Errorf("ActProject(%s) ran %v", c.project, f.acted())
		}
	}
	if _, err := ActProject(ctx, (&fake{}).run, p, "shop", "restart"); err == nil {
		t.Error("restart should be refused")
	}

	mux := http.NewServeMux()
	f = &fake{}
	Register(mux, &Service{Docker: f.run, Policy: p})
	for path, code := range map[string]int{
		"/api/docker/projects/shop/stop":  200,
		"/api/docker/projects/ci/kill":    400,
		"/api/docker/projects/nope/start": 404,
	} {
		req := httptest.NewRequest("POST", path, nil)
		req.Header.Set("X-Lucid-Confirm", "yes")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != code {
			t.Errorf("POST %s = %d, want %d (%s)", path, rec.Code, code, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/docker/projects/shop/stop", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("without confirm: %d", rec.Code)
	}
}
