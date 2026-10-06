package picture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/databases"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The test binary doubles as the claude CLI: copied onto PATH as "claude" and
// run with LUCID_FAKE_PICTURE set, it records the prompt it was given on
// stdin and the arguments it was started with, and answers with a reply.
func TestMain(m *testing.M) {
	if dir := os.Getenv("LUCID_FAKE_PICTURE"); dir != "" {
		in, _ := io.ReadAll(os.Stdin)
		_ = os.WriteFile(filepath.Join(dir, "prompt.txt"), in, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "args.txt"), []byte(strings.Join(os.Args[1:], "\x00")), 0o600)
		reply, _ := os.ReadFile(filepath.Join(dir, "reply.txt"))
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": string(reply), "total_cost_usd": 0.0123,
			"usage": map[string]any{"input_tokens": 100, "output_tokens": 50}, "modelUsage": map[string]any{"claude-haiku-test": map[string]any{}}})
		fmt.Println(string(b))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeClaude puts a fake claude on a fresh PATH and returns its record dir.
func fakeClaude(t *testing.T, reply string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin, rec := t.TempDir(), t.TempDir()
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rec, "reply.txt"), []byte(reply), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("LUCID_FAKE_PICTURE", rec)
	return rec
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	v, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Store{Vault: func() (*memory.Vault, error) { return v, nil }}
}

func TestLocateRepoVersusMemory(t *testing.T) {
	s := newStore(t)
	repo := t.TempDir()
	with := projects.Project{ID: "app", LocalPath: repo}
	loc := s.Locate(with)
	if loc.Where != WhereRepo || loc.Dir != filepath.Join(repo, "docs", "picture") {
		t.Errorf("repo location = %+v", loc)
	}
	for name, p := range map[string]projects.Project{
		"no path":      {ID: "app"},
		"missing path": {ID: "app", LocalPath: filepath.Join(repo, "nope")},
		"a file":       {ID: "app", LocalPath: filepath.Join(repo, "f")},
	} {
		write(t, repo, "f", "x")
		got := s.Locate(p)
		if got.Where != WhereMemory || got.Dir != "Picture/app" {
			t.Errorf("%s: location = %+v, want Memory Picture/app", name, got)
		}
	}
}

func TestRoundTripRepoAndMemory(t *testing.T) {
	s := newStore(t)
	repo := t.TempDir()
	for _, p := range []projects.Project{{ID: "app", LocalPath: repo}, {ID: "loose"}} {
		for kind, content := range map[string]string{KindMermaid: "flowchart LR\n  a --> b\n", KindExcalidraw: `{"type":"excalidraw","elements":[]}`} {
			it, err := s.Write(p, "design", kind, content)
			if err != nil {
				t.Fatalf("%s %s: write: %v", p.ID, kind, err)
			}
			if p.LocalPath != "" && it.Where != WhereRepo || p.LocalPath == "" && it.Where != WhereMemory {
				t.Errorf("%s: wrote to %s", p.ID, it.Where)
			}
			got, err := s.Read(p, "design", kind)
			if err != nil || strings.TrimSpace(got) != strings.TrimSpace(content) {
				t.Errorf("%s %s: read = %q, %v", p.ID, kind, got, err)
			}
		}
		items, err := s.List(p)
		if err != nil || len(items) != 2 {
			t.Errorf("%s: list = %+v, %v", p.ID, items, err)
		}
		if _, err := s.Read(p, "absent", KindMermaid); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: missing read err = %v", p.ID, err)
		}
	}
	for _, f := range []string{"docs/picture/design.mmd", "docs/picture/design.excalidraw"} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(f))); err != nil {
			t.Errorf("repo file %s: %v", f, err)
		}
	}
	// Memory keeps a Markdown page, so the vault stays Markdown.
	v, _ := s.Vault()
	pg, err := v.Read("Picture/loose/design.mmd.md")
	if err != nil || !strings.Contains(pg.Body, "```mermaid") || pg.Front["picture"] != KindMermaid {
		t.Errorf("memory page = %+v, %v", pg, err)
	}
}

func TestNamesCannotEscape(t *testing.T) {
	s := newStore(t)
	repo := t.TempDir()
	p := projects.Project{ID: "app", LocalPath: repo}
	for _, name := range []string{"", ".", "..", "../x", "a/b", `a\b`, "/abs", "C:evil", "x.mmd", "..\\..\\evil", "con", "nul", "com1", "aux-1", "UPPER", " sp", "a b", strings.Repeat("a", 64), "a\x00b"} {
		if _, err := s.Write(p, name, KindMermaid, "x"); !errors.Is(err, ErrBadPath) {
			t.Errorf("Write(%q) err = %v, want ErrBadPath", name, err)
		}
		if _, err := s.Read(p, name, KindMermaid); !errors.Is(err, ErrBadPath) {
			t.Errorf("Read(%q) err = %v, want ErrBadPath", name, err)
		}
	}
	if _, err := s.Write(p, "ok", "pdf", "x"); !errors.Is(err, ErrBadPath) {
		t.Errorf("unknown kind err = %v", err)
	}
	if _, err := s.Write(p, "ok", KindMermaid, strings.Repeat("x", MaxSize+1)); !errors.Is(err, ErrTooBig) {
		t.Errorf("oversize err = %v", err)
	}
	// Nothing was written outside docs/picture.
	var stray []string
	_ = filepath.WalkDir(repo, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			stray = append(stray, path)
		}
		return nil
	})
	if len(stray) != 0 {
		t.Errorf("files written: %v", stray)
	}
}

func TestLinkedDocsFolderIsRefused(t *testing.T) {
	s := newStore(t)
	repo, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, "docs")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	p := projects.Project{ID: "app", LocalPath: repo}
	if _, err := s.Write(p, "x", KindMermaid, "flowchart LR"); !errors.Is(err, ErrBadPath) {
		t.Errorf("write through a linked docs = %v, want ErrBadPath", err)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("wrote outside the repo: %v", ents)
	}
}

func TestSelectAndGenerateGolden(t *testing.T) {
	p := projects.Project{
		ID: "shop", Name: `Shop "Pro"`, Type: "web-app", Status: "active", Repo: "me/shop", LocalPath: filepath.Join("work", "Shop-Repo"),
		BuildsInto: []string{"portal"}, BuiltBy: []string{"kit"},
		Deploy: []projects.Deploy{{Provider: "wrangler", Service: "shop-web"}, {Provider: "gcloud", Service: "shop-api", Region: "us-central1"}},
	}
	cs := []docker.Container{
		{Name: "shop-api-1", Service: "api", Image: "shop/api:1", State: "running", Project: "shop"},
		{Name: "shop-db-1", Service: "db", Image: "postgres:16", State: "running", Project: "shop", Ports: "0.0.0.0:5433->5432/tcp"},
		{Name: "web-1", Service: "web", Image: "nginx", State: "exited", Project: "shop-repo"},
		{Name: "other-1", Service: "x", Image: "x", State: "running", Project: "other"},
	}
	dbs := []databases.Discovered{
		{Name: "shop-db-1", Engine: "postgres", Port: 5433, Compose: "shop"},
		{Name: "cache", Engine: "redis", Port: 6379},
		{Name: "elsewhere", Engine: "mysql", Port: 3306, Compose: "other"},
	}
	rs := []ci.Container{
		{Name: "runner-a", RunnerName: "shop-runner", State: "running", RepoURL: "https://github.com/me/shop"},
		{Name: "runner-b", State: "running", RepoURL: "https://github.com/me/other"},
	}
	in := Select(p, cs, dbs, rs)
	if len(in.Containers) != 3 || len(in.Databases) != 1 || len(in.Runners) != 1 {
		t.Fatalf("selected %d containers, %d databases, %d runners; want 3, 1, 1", len(in.Containers), len(in.Databases), len(in.Runners))
	}
	in.Names = map[string]string{"portal": "Portal", "kit": "Kit"}
	got := Generate(in)
	want := `%% Generated from live state by Lucidbench. Edit freely.
flowchart LR
  p_main["Shop #quot;Pro#quot; · web-app · active"]
  repo[("me/shop")]
  p_main --- repo
  subgraph deploys ["Deploys"]
    d_1["gcloud · shop-api · us-central1"]
    d_2["wrangler · shop-web"]
  end
  p_main --> d_1
  p_main --> d_2
  subgraph compose ["Containers"]
    c_1["api · shop/api:1 · running"]
    c_2["web · nginx · exited"]
  end
  p_main --> c_1
  p_main --> c_2
  subgraph data ["Databases"]
    db_1[("shop-db-1 · postgres:5433")]
  end
  p_main -.-> db_1
  subgraph ci ["CI runners"]
    r_1["shop-runner · running"]
  end
  repo --> r_1
  o_1["Portal"]
  p_main -->|builds into| o_1
  u_1["Kit"]
  u_1 -->|builds into| p_main
`
	if got != want {
		t.Errorf("diagram:\n%s\nwant:\n%s", got, want)
	}
	// The same inputs give the same text.
	if again := Generate(in); again != got {
		t.Error("Generate is not deterministic")
	}
}

func TestGenerateSparseAndPortLink(t *testing.T) {
	got := Generate(Inputs{Project: projects.Project{ID: "bare", Name: "Bare"}})
	if !strings.Contains(got, `p_main["Bare"]`) || !strings.Contains(got, "Nothing else is known") {
		t.Errorf("sparse diagram:\n%s", got)
	}
	// A database with no compose project is linked by a port the project's
	// containers publish.
	p := projects.Project{ID: "app"}
	in := Select(p,
		[]docker.Container{{Name: "app-1", Project: "app", Ports: "127.0.0.1:5432->5432/tcp"}},
		[]databases.Discovered{{Name: "pg", Engine: "postgres", Port: 5432}, {Name: "pg2", Engine: "postgres", Port: 15432}}, nil)
	if len(in.Databases) != 1 || in.Databases[0].Name != "pg" {
		t.Errorf("port-linked databases = %+v", in.Databases)
	}
}

func draftProject(t *testing.T) projects.Project {
	root := t.TempDir()
	write(t, root, "README.md", "# Demo\nIt talks to a queue.\n")
	write(t, root, "cmd/api/main.go", "package main // SENTINEL-FILE-CONTENT")
	write(t, root, "internal/store/db.go", "x")
	write(t, root, "a/b/c/deep.go", "x")
	write(t, root, "a/b/c/d/too-deep.go", "x")
	write(t, root, ".env", "TOKEN=SENTINEL-ENV")
	write(t, root, "node_modules/x/y.js", "x")
	write(t, root, "dist/out.js", "x")
	write(t, root, ".git/config", "x")
	return projects.Project{ID: "demo", Name: "Demo", LocalPath: root, Visibility: "private"}
}

func TestDraftPromptIsTreeAndReadmeOnly(t *testing.T) {
	p := draftProject(t)
	rec := fakeClaude(t, "Here you go:\n```mermaid\nflowchart LR\n  api --> queue\n```\nEnjoy.")
	res, err := Draft(context.Background(), &agentexec.Runner{}, p, DraftRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mermaid != "flowchart LR\n  api --> queue\n" || res.Provider != "claude" {
		t.Errorf("result = %+v", res)
	}
	if res.Usage.CostUSD != 0.0123 {
		t.Errorf("cost = %v", res.Usage.CostUSD)
	}
	b, _ := os.ReadFile(filepath.Join(rec, "prompt.txt"))
	prompt := string(b)
	for _, want := range []string{"cmd/api/main.go", "internal/store/", "a/b/c/", "# Demo\nIt talks to a queue.\n", "README.md"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	for _, bad := range []string{"SENTINEL", ".env", "node_modules", "dist/", ".git/", "too-deep", p.LocalPath} {
		if strings.Contains(prompt, bad) {
			t.Errorf("prompt contains %q:\n%s", bad, prompt)
		}
	}
	// Exactly the two sections, nothing else.
	if prompt != DraftPrompt(Tree(p.LocalPath), ReadmeHead(p.LocalPath)) {
		t.Errorf("prompt is not the tree and README excerpt:\n%s", prompt)
	}
	args, _ := os.ReadFile(filepath.Join(rec, "args.txt"))
	if !strings.Contains(string(args), "haiku") {
		t.Errorf("default model is not haiku: %q", args)
	}
}

func TestReadmeHeadCapsLines(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	write(t, root, "readme.md", sb.String())
	got := ReadmeHead(root)
	if !strings.Contains(got, "line 200\n") || strings.Contains(got, "line 201") {
		t.Errorf("README head is not 200 lines: %d bytes", len(got))
	}
}

func TestDraftRefusesConfidentialAndUnreadable(t *testing.T) {
	rec := fakeClaude(t, "flowchart LR")
	p := draftProject(t)
	p.Visibility = "confidential"
	if _, err := Draft(context.Background(), &agentexec.Runner{}, p, DraftRequest{}); !errors.Is(err, ErrConfidential) {
		t.Errorf("confidential err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(rec, "prompt.txt")); err == nil {
		t.Error("the CLI was run for a confidential project")
	}
	if _, err := Draft(context.Background(), nil, projects.Project{ID: "x"}, DraftRequest{}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("no local_path err = %v", err)
	}
	if _, err := Draft(context.Background(), nil, draftProject(t), DraftRequest{Provider: "gpt"}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("bad provider err = %v", err)
	}
}

func TestDraftWithNoCLI(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := Draft(context.Background(), &agentexec.Runner{}, draftProject(t), DraftRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no CLI err = %v", err)
	}
}

type api struct {
	t   *testing.T
	mux *http.ServeMux
	svc *Service
}

func newAPI(t *testing.T, yaml string) *api {
	t.Helper()
	pf := filepath.Join(t.TempDir(), "projects.yaml")
	if err := os.WriteFile(pf, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Projects: func() (*projects.List, error) { return projects.LoadFrom(pf, ""), nil },
		Store:    newStore(t),
		Runner:   &agentexec.Runner{},
	}
	mux := http.NewServeMux()
	Register(mux, svc)
	return &api{t: t, mux: mux, svc: svc}
}

func (a *api) do(method, path, body string, confirm bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	w := httptest.NewRecorder()
	a.mux.ServeHTTP(w, req)
	return w
}

func TestHTTP(t *testing.T) {
	repo := t.TempDir()
	a := newAPI(t, `version: 1
projects:
  - {id: app, name: App, category: product, type: web-app, status: active, visibility: private, local_path: `+jsonPath(repo)+`}
  - {id: loose, name: Loose, category: product, type: web-app, status: active, visibility: private}
  - {id: secret, name: Secret, category: product, type: web-app, status: active, visibility: confidential, local_path: `+jsonPath(repo)+`}
`)
	if w := a.do("PUT", "/api/picture/app/item?name=arch&kind=mermaid", `{"content":"flowchart LR\n a-->b"}`, false); w.Code != 403 && w.Code != 400 {
		t.Errorf("unconfirmed PUT = %d", w.Code)
	}
	w := a.do("GET", "/api/picture/app/target?name=arch&kind=mermaid", "", false)
	var tg map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &tg)
	if w.Code != 200 || tg["path"] != filepath.Join(repo, "docs", "picture", "arch.mmd") || tg["where"] != "repo" {
		t.Errorf("target = %d %s", w.Code, w.Body)
	}
	if w := a.do("PUT", "/api/picture/app/item?name=arch&kind=mermaid", `{"content":"flowchart LR\n a-->b"}`, true); w.Code != 200 {
		t.Fatalf("PUT = %d %s", w.Code, w.Body)
	}
	if w := a.do("PUT", "/api/picture/app/item?name=..%2Fevil&kind=mermaid", `{"content":"x"}`, true); w.Code != 400 {
		t.Errorf("escaping PUT = %d %s", w.Code, w.Body)
	}
	if w := a.do("PUT", "/api/picture/app/item?name=ok&kind=mermaid", `{"content":"x","extra":1}`, true); w.Code != 400 {
		t.Errorf("unknown field PUT = %d", w.Code)
	}
	w = a.do("GET", "/api/picture/app", "", false)
	var l Listing
	_ = json.Unmarshal(w.Body.Bytes(), &l)
	if w.Code != 200 || len(l.Items) != 1 || l.Items[0].Name != "arch" || !l.CanDraft || l.Where != "repo" {
		t.Errorf("listing = %d %s", w.Code, w.Body)
	}
	if w := a.do("GET", "/api/picture/app/item?name=arch&kind=mermaid", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), "a--") {
		t.Errorf("read = %d %s", w.Code, w.Body)
	}
	if w := a.do("GET", "/api/picture/app/item?name=none&kind=mermaid", "", false); w.Code != 404 {
		t.Errorf("missing read = %d", w.Code)
	}
	if w := a.do("GET", "/api/picture/nope", "", false); w.Code != 404 {
		t.Errorf("unknown project = %d", w.Code)
	}
	// A project with no repo lists Memory and cannot be drafted.
	w = a.do("GET", "/api/picture/loose", "", false)
	l = Listing{}
	_ = json.Unmarshal(w.Body.Bytes(), &l)
	if w.Code != 200 || l.Where != "memory" || l.CanDraft || l.Dir != "Picture/loose" {
		t.Errorf("loose listing = %d %s", w.Code, w.Body)
	}
	// Confidential: listed and writable by hand, but never drafted.
	t.Setenv("PATH", "")
	if w := a.do("POST", "/api/picture/secret/draft", `{}`, true); w.Code != 403 {
		t.Errorf("confidential draft = %d %s", w.Code, w.Body)
	}
	if w := a.do("POST", "/api/picture/app/draft", `{}`, false); w.Code == 200 {
		t.Errorf("unconfirmed draft = %d", w.Code)
	}
	if w := a.do("GET", "/api/picture/secret", "", false); !strings.Contains(w.Body.String(), `"can_draft":false`) {
		t.Errorf("confidential listing offers drafts: %s", w.Body)
	}
	w = a.do("GET", "/api/picture/app/live", "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "flowchart LR") {
		t.Errorf("live = %d %s", w.Code, w.Body)
	}
	if w := a.do("GET", "/api/picture", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), `"project":"app","name":"App","where":"repo","count":1`) {
		t.Errorf("summary = %d %s", w.Code, w.Body)
	}
}

func jsonPath(p string) string {
	b, _ := json.Marshal(p)
	return string(b)
}
