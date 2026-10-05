package hostinfo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTidy(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "you")
	cases := map[string]string{
		filepath.Join(home, "AppData", "lucidbench"): "~/AppData/lucidbench",
		home: "~",
		filepath.Join(string(filepath.Separator)+"etc", "lucid"): filepath.Join(string(filepath.Separator)+"etc", "lucid"),
	}
	for in, want := range cases {
		if got := Tidy(in, home); got != want {
			t.Errorf("Tidy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolsReportsPresenceOnly(t *testing.T) {
	d := &Detector{
		LookPath: func(n string) (string, error) {
			if n == "gh" || n == "stripe" {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		},
		Getenv:   func(n string) string { return map[string]string{"TRELLO_API_KEY": "s3cret"}[n] },
		DockerUp: func(context.Context) bool { return true },
	}
	rec := httptest.NewRecorder()
	ToolsHandler(d).ServeHTTP(rec, httptest.NewRequest("GET", "/api/host/tools", nil))
	if strings.Contains(rec.Body.String(), "s3cret") || strings.Contains(rec.Body.String(), "/usr/bin") {
		t.Fatalf("leaks values: %s", rec.Body)
	}
	var tools Tools
	if err := json.Unmarshal(rec.Body.Bytes(), &tools); err != nil {
		t.Fatal(err)
	}
	if !tools.CLIs["gh"] || !tools.CLIs["stripe"] || tools.CLIs["gcloud"] || !tools.Env["TRELLO_API_KEY"] || !tools.Docker {
		t.Errorf("tools = %+v", tools)
	}
}
