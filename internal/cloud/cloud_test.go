package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The test binary doubles as a fake CLI: copied onto PATH under a CLI's name
// and run with LUCID_FAKE_CLOUD set, it prints canned output.
func TestMain(m *testing.M) {
	if out := os.Getenv("LUCID_FAKE_CLOUD"); out != "" {
		_, _ = io.WriteString(os.Stdout, out)
		_, _ = io.WriteString(os.Stderr, os.Getenv("LUCID_FAKE_CLOUD_ERR"))
		if os.Getenv("LUCID_FAKE_CLOUD_FAIL") != "" {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// reply is one canned CLI answer.
type reply struct {
	out, err string
	fail     bool
}

// fake answers by "cli args" and counts every call.
type fake struct {
	mu      sync.Mutex
	replies map[string]reply
	calls   []string
}

func (f *fake) run(_ context.Context, name string, args ...string) ([]byte, string, error) {
	key := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, key)
	r, ok := f.replies[key]
	f.mu.Unlock()
	if !ok {
		return nil, "unexpected call: " + key, errors.New("exit status 2")
	}
	if r.fail {
		return []byte(r.out), r.err, errors.New("exit status 1")
	}
	return []byte(r.out), r.err, nil
}

func (f *fake) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func svc(f *fake, ps ...projects.Project) *Service {
	return &Service{
		Run:      f.run,
		LookPath: func(string) (string, error) { return "/bin/x", nil },
		Projects: func() []projects.Project { return ps },
		TTL:      DefaultTTL,
		Now:      time.Now,
	}
}

func get(t *testing.T, s *Service, id string) Summary {
	t.Helper()
	sum, ok := s.Get(context.Background(), id, false)
	if !ok {
		t.Fatalf("unknown provider %s", id)
	}
	return sum
}

func section(t *testing.T, sum Summary, id string) Section {
	t.Helper()
	for _, s := range sum.Sections {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no section %q in %+v", id, sum.Sections)
	return Section{}
}

func byName(t *testing.T, sec Section, name string) Resource {
	t.Helper()
	for _, r := range sec.Resources {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no resource %q in section %s: %+v", name, sec.ID, sec.Resources)
	return Resource{}
}

const (
	gConfig = `{"core":{"account":"dev@example.com","project":"demo-proj"},"run":{"region":"us-central1"}}`
	gProjs  = `[{"projectId":"demo-proj","name":"Demo","lifecycleState":"ACTIVE","projectNumber":"123"},{"projectId":"other-proj","name":"Other","lifecycleState":"ACTIVE"}]`
	gRun    = `[
 {"metadata":{"name":"api","creationTimestamp":"2026-01-01T00:00:00Z","labels":{"cloud.googleapis.com/location":"us-central1"}},
  "spec":{"template":{"spec":{"containers":[{"env":[{"name":"DB_PASSWORD","value":"hunter2-secret-env"}]}]}}},
  "status":{"url":"https://api-abc.a.run.app","latestReadyRevisionName":"api-00007-xyz","conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-09-30T10:00:00Z"}]}},
 {"metadata":{"name":"web","labels":{"cloud.googleapis.com/location":"europe-west1"}},
  "status":{"url":"https://web-abc.a.run.app","latestCreatedRevisionName":"web-00002-bad","conditions":[{"type":"Ready","status":"False","lastTransitionTime":"2026-10-01T10:00:00Z","message":"container failed to start"}]}}
]`
)

func gcloudFake() *fake {
	return &fake{replies: map[string]reply{
		"gcloud config list --format=json":                                    {out: gConfig},
		"gcloud projects list --limit=100 --format=json":                      {out: gProjs},
		"gcloud run services list --project demo-proj --quiet --format=json":  {out: gRun},
		"gcloud run services list --project other-proj --quiet --format=json": {out: "[]"},
	}}
}

func TestGcloudInventory(t *testing.T) {
	s := svc(gcloudFake())
	sum := get(t, s, "gcloud")
	if sum.State != StateConnected || sum.Account != "dev@example.com" || sum.Scope != "demo-proj" {
		t.Fatalf("summary = %+v", sum)
	}
	run := section(t, sum, "run:demo-proj")
	api := byName(t, run, "api")
	if api.Status != StatusReady || api.Region != "us-central1" || api.URL != "https://api-abc.a.run.app" || api.Detail != "revision api-00007-xyz" || api.Updated != "2026-09-30T10:00:00Z" {
		t.Errorf("api = %+v", api)
	}
	if !strings.HasPrefix(api.Console, "https://console.cloud.google.com/run/detail/us-central1/api/metrics?project=demo-proj") {
		t.Errorf("console = %s", api.Console)
	}
	if web := byName(t, run, "web"); web.Status != StatusFailed || web.Region != "europe-west1" || web.Detail != "revision web-00002-bad" {
		t.Errorf("web = %+v", web)
	}
	if got := section(t, sum, "projects").Resources; len(got) != 2 || got[0].Kind != KindGCPProject || got[0].Status != "" {
		t.Errorf("projects = %+v", got)
	}
	if sum.Counts != (Counts{Ready: 1, Failed: 1}) {
		t.Errorf("counts = %+v", sum.Counts)
	}
}

func TestGcloudNotSignedIn(t *testing.T) {
	f := &fake{replies: map[string]reply{"gcloud config list --format=json": {out: `{"core":{}}`}}}
	if sum := get(t, svc(f), "gcloud"); sum.State != StateNotSignedIn || sum.Message != "not signed in" || sum.Login != "gcloud auth login" {
		t.Errorf("summary = %+v", sum)
	}
	// Signed in at config level, but the token no longer works.
	f = gcloudFake()
	f.replies["gcloud projects list --limit=100 --format=json"] = reply{fail: true, err: "ERROR: (gcloud.projects.list) Reauthentication failed. Please run: gcloud auth login"}
	if sum := get(t, svc(f), "gcloud"); sum.State != StateNotSignedIn {
		t.Errorf("expired token = %+v", sum)
	}
}

func TestGcloudSectionsFailAlone(t *testing.T) {
	f := gcloudFake()
	f.replies["gcloud run services list --project demo-proj --quiet --format=json"] = reply{fail: true, err: "ERROR: (gcloud.run.services.list) PERMISSION_DENIED: Cloud Run Admin API has not been used in project demo-proj"}
	sum := get(t, svc(f), "gcloud")
	if sum.State != StateConnected {
		t.Fatalf("state = %s (%s)", sum.State, sum.Message)
	}
	if run := section(t, sum, "run:demo-proj"); !strings.Contains(run.Error, "PERMISSION_DENIED") || len(run.Resources) != 0 {
		t.Errorf("run section = %+v", run)
	}
	if got := section(t, sum, "projects").Resources; len(got) != 2 {
		t.Errorf("projects still load: %+v", got)
	}
}

func TestGcloudNoActiveProject(t *testing.T) {
	f := &fake{replies: map[string]reply{
		"gcloud config list --format=json":               {out: `{"core":{"account":"dev@example.com"}}`},
		"gcloud projects list --limit=100 --format=json": {out: "[]"},
	}}
	sum := get(t, svc(f), "gcloud")
	if run := section(t, sum, "run"); !strings.Contains(run.Note, "No active project") {
		t.Errorf("run = %+v", run)
	}
}

const (
	wWho    = `{"loggedIn":true,"authType":"OAuth Token","email":"dev@example.com","accounts":[{"id":"acct123","name":"Dev Account","type":"standard"}],"tokenPermissions":["workers:write","secret-scope"]}`
	wPages  = `[{"Project Name":"site","Project Domains":"site.pages.dev, www.example.dev","Git Provider":"No","Last Modified":"6 days ago"},{"Project Name":"docs","Project Domains":"docs.pages.dev","Git Provider":"Yes","Last Modified":"1 week ago"}]`
	wDeploy = `[{"Id":"d1","Environment":"Production","Branch":"main","Source":"abc1234","Deployment":"https://d1.site.pages.dev","Status":"Failure","Build":"https://dash.cloudflare.com/acct123/pages/view/site/d1"}]`
	wWorker = `[{"id":"x","source":"wrangler","created_on":"2026-09-01T00:00:00Z","author_email":"dev@example.com","versions":[{"version_id":"v","percentage":100}]},{"id":"y","created_on":"2026-10-02T08:00:00Z"}]`
)

func wranglerFake() *fake {
	return &fake{replies: map[string]reply{
		"wrangler whoami --json":                                    {out: wWho},
		"wrangler pages project list --json":                        {out: wPages},
		"wrangler pages deployment list --project-name site --json": {out: wDeploy},
		"wrangler deployments list --name edge --json":              {out: wWorker},
		"wrangler deployments list --name ghost --json":             {fail: true, err: "\x1b[31m✘ [ERROR]\x1b[0m A request failed.\n\n  This Worker does not exist on your account. [code: 10007]"},
	}}
}

func deployTargets(ds ...projects.Deploy) projects.Project {
	return projects.Project{ID: "app", Name: "App", Deploy: ds}
}

func TestWranglerInventory(t *testing.T) {
	s := svc(wranglerFake(), deployTargets(
		projects.Deploy{Provider: "wrangler", Service: "site"},
		projects.Deploy{Provider: "wrangler", Service: "edge"},
		projects.Deploy{Provider: "wrangler", Service: "ghost"},
	))
	sum := get(t, s, "wrangler")
	if sum.State != StateConnected || sum.Account != "Dev Account" {
		t.Fatalf("summary = %+v", sum)
	}
	pages := section(t, sum, "pages")
	site := byName(t, pages, "site")
	if site.Status != StatusFailed || site.URL != "https://site.pages.dev" || site.Detail != "production · main · abc1234" || !strings.HasSuffix(site.Console, "/site/d1") {
		t.Errorf("site = %+v", site)
	}
	// Not named in projects.yaml: listed, but its health is unknown.
	if docs := byName(t, pages, "docs"); docs.Status != StatusUnknown || docs.UpdatedText != "1 week ago" || !strings.Contains(docs.Detail, "projects.yaml") {
		t.Errorf("docs = %+v", docs)
	}
	workers := section(t, sum, "workers")
	if edge := byName(t, workers, "edge"); edge.Status != StatusReady || edge.Updated != "2026-10-02T08:00:00Z" || edge.Detail != "2 recent deployments" {
		t.Errorf("edge = %+v", edge)
	}
	if ghost := byName(t, workers, "ghost"); ghost.Status != StatusUnknown || ghost.Detail != "not found on this account" {
		t.Errorf("ghost = %+v", ghost)
	}
	if !strings.Contains(workers.Note, "cannot list Workers") {
		t.Errorf("note = %q", workers.Note)
	}
	if sum.Counts != (Counts{Ready: 1, Failed: 1}) {
		t.Errorf("counts = %+v", sum.Counts)
	}
}

func TestWranglerNotSignedIn(t *testing.T) {
	for name, r := range map[string]reply{
		"logged out json": {out: `{"loggedIn":false}`},
		"nonzero":         {fail: true, err: "✘ [ERROR] You are not authenticated. Please run `wrangler login`."},
	} {
		f := &fake{replies: map[string]reply{"wrangler whoami --json": r}}
		if sum := get(t, svc(f), "wrangler"); sum.State != StateNotSignedIn {
			t.Errorf("%s: %+v", name, sum)
		}
	}
}

const (
	vWho   = `{"team":{"id":"team_1","slug":"acme","name":"Acme"},"username":"dev-user","email":"dev@example.com","name":null}`
	vProjs = `{"projects":[{"name":"web","id":"p1","latestProductionUrl":"web.example.dev","updatedAt":1790000000000},{"name":"api","id":"p2","latestProductionUrl":"","updatedAt":1780000000000},{"name":"idle","id":"p3","updatedAt":1770000000000}]}`
	vDeps  = `{"deployments":[
  {"url":"web-1.vercel.app","name":"web","state":"READY","target":"production","createdAt":1790000000000,"meta":{"githubCommitRef":"main","githubCommitAuthorEmail":"dev@example.com"}},
  {"url":"api-2.vercel.app","name":"api","state":"BLOCKED","target":"production","createdAt":1789000000000,"meta":{"githubCommitRef":"main"}},
  {"url":"api-1.vercel.app","name":"api","state":"READY","target":"production","createdAt":1788000000000},
  {"url":"web-0.vercel.app","name":"web","state":"BUILDING","target":"preview","createdAt":1791000000000}
 ]}`
)

func vercelFake() *fake {
	hint := `<claude-code-hint v="1" type="plugin" value="vercel@claude-plugins-official" />`
	return &fake{replies: map[string]reply{
		"vercel whoami --format json":              {out: vWho, err: hint},
		"vercel project ls --format json":          {out: vProjs, err: hint},
		"vercel ls --all --limit 50 --format json": {out: vDeps, err: hint},
	}}
}

func TestVercelInventory(t *testing.T) {
	sum := get(t, svc(vercelFake()), "vercel")
	if sum.State != StateConnected || sum.Account != "dev-user" || sum.Scope != "team acme" {
		t.Fatalf("summary = %+v", sum)
	}
	pj := section(t, sum, "projects")
	// The newest production deployment decides, not a newer preview.
	if web := byName(t, pj, "web"); web.Status != StatusReady || web.URL != "https://web.example.dev" || web.Detail != "ready · production · main" || web.Console != "https://vercel.com/acme/web" {
		t.Errorf("web = %+v", web)
	}
	if api := byName(t, pj, "api"); api.Status != StatusFailed || !strings.HasPrefix(api.Detail, "blocked") {
		t.Errorf("api = %+v", api)
	}
	if idle := byName(t, pj, "idle"); idle.Status != "" {
		t.Errorf("idle = %+v", idle)
	}
	dp := section(t, sum, "deployments")
	if len(dp.Resources) != 4 || dp.Resources[0].Detail != "building · preview" || dp.Resources[0].URL != "https://web-0.vercel.app" {
		t.Errorf("deployments = %+v", dp.Resources)
	}
	if sum.Counts != (Counts{Ready: 1, Failed: 1}) {
		t.Errorf("deployment rows must not be counted: %+v", sum.Counts)
	}
}

func TestVercelNotSignedIn(t *testing.T) {
	f := &fake{replies: map[string]reply{"vercel whoami --format json": {fail: true, err: "Error: No existing credentials found. Please run `vercel login` or pass \"--token\""}}}
	if sum := get(t, svc(f), "vercel"); sum.State != StateNotSignedIn {
		t.Errorf("summary = %+v", sum)
	}
}

func TestAzAndAws(t *testing.T) {
	f := &fake{replies: map[string]reply{
		"az account show --output json":             {out: `{"id":"sub-1","name":"Dev Subscription","user":{"name":"dev@example.com","type":"user"},"tenantId":"tenant-secret"}`},
		"aws sts get-caller-identity --output json": {out: `{"UserId":"AIDASECRET","Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/dev"}`},
	}}
	s := svc(f)
	if sum := get(t, s, "az"); sum.State != StateConnected || sum.Account != "Dev Subscription" || len(sum.Sections) != 0 {
		t.Errorf("az = %+v", sum)
	}
	if sum := get(t, s, "aws"); sum.State != StateConnected || sum.Account != "123456789012" {
		t.Errorf("aws = %+v", sum)
	}
	f = &fake{replies: map[string]reply{
		"az account show --output json":             {fail: true, err: "ERROR: Please run 'az login' to setup account."},
		"aws sts get-caller-identity --output json": {fail: true, err: "Unable to locate credentials. You can configure credentials by running \"aws configure\"."},
	}}
	s = svc(f)
	for _, id := range []string{"az", "aws"} {
		if sum := get(t, s, id); sum.State != StateNotSignedIn {
			t.Errorf("%s = %+v", id, sum)
		}
	}
	f = &fake{replies: map[string]reply{"aws sts get-caller-identity --output json": {fail: true, err: "Could not connect to the endpoint URL"}}}
	if sum := get(t, svc(f), "aws"); sum.State != StateError || !strings.Contains(sum.Message, "Could not connect") {
		t.Errorf("aws error = %+v", sum)
	}
}

func TestMissingCLI(t *testing.T) {
	s := svc(&fake{})
	s.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	for _, id := range ProviderIDs() {
		sum := get(t, s, id)
		if sum.State != StateNotInstalled || !strings.Contains(sum.Message, "not installed") || len(sum.Sections) != 0 {
			t.Errorf("%s = %+v", id, sum)
		}
	}
}

func TestCommandErrorAndTimeout(t *testing.T) {
	f := &fake{replies: map[string]reply{"gcloud config list --format=json": {fail: true, err: "ERROR: something odd happened"}}}
	if sum := get(t, svc(f), "gcloud"); sum.State != StateError || sum.Message != "ERROR: something odd happened" {
		t.Errorf("error = %+v", sum)
	}
	f = &fake{replies: map[string]reply{"gcloud config list --format=json": {out: "not json at all"}}}
	if sum := get(t, svc(f), "gcloud"); sum.State != StateError || !strings.Contains(sum.Message, "no JSON") {
		t.Errorf("garbage = %+v", sum)
	}

	old := callTimeout
	callTimeout = 50 * time.Millisecond
	defer func() { callTimeout = old }()
	s := svc(&fake{})
	s.Run = func(ctx context.Context, _ string, _ ...string) ([]byte, string, error) {
		<-ctx.Done()
		return nil, "", ctx.Err()
	}
	if sum := get(t, s, "aws"); sum.State != StateError || !strings.Contains(sum.Message, "timed out") {
		t.Errorf("timeout = %+v", sum)
	}
}

func TestCache(t *testing.T) {
	f := vercelFake()
	now := time.Now()
	s := svc(f)
	s.Now = func() time.Time { return now }
	get(t, s, "vercel")
	get(t, s, "vercel")
	if n := f.count("vercel whoami"); n != 1 {
		t.Fatalf("second read within the TTL ran the CLI again (%d calls)", n)
	}
	now = now.Add(4 * time.Minute)
	get(t, s, "vercel")
	if n := f.count("vercel whoami"); n != 1 {
		t.Fatalf("still cached at 4 minutes, got %d calls", n)
	}
	now = now.Add(2 * time.Minute)
	get(t, s, "vercel")
	if n := f.count("vercel whoami"); n != 2 {
		t.Fatalf("expired after 5 minutes, got %d calls", n)
	}
	if _, _ = s.Get(context.Background(), "vercel", true); f.count("vercel whoami") != 3 {
		t.Fatal("refresh did not run the CLI")
	}
}

func TestCacheShortForFailures(t *testing.T) {
	f := &fake{replies: map[string]reply{"az account show --output json": {fail: true, err: "Please run 'az login'"}}}
	now := time.Now()
	s := svc(f)
	s.Now = func() time.Time { return now }
	get(t, s, "az")
	now = now.Add(10 * time.Second)
	get(t, s, "az")
	if f.count("az ") != 1 {
		t.Fatal("a failure should be reused briefly")
	}
	now = now.Add(time.Minute)
	f.mu.Lock()
	f.replies["az account show --output json"] = reply{out: `{"name":"Sub"}`}
	f.mu.Unlock()
	if sum := get(t, s, "az"); sum.State != StateConnected {
		t.Fatalf("a sign-in should show up within a minute: %+v", sum)
	}
}

func TestCacheFollowsDeployEntries(t *testing.T) {
	f := wranglerFake()
	ps := []projects.Project{deployTargets(projects.Deploy{Provider: "wrangler", Service: "edge"})}
	s := svc(f)
	s.Projects = func() []projects.Project { return ps }
	get(t, s, "wrangler")
	get(t, s, "wrangler")
	if f.count("wrangler whoami") != 1 {
		t.Fatal("unchanged entries should hit the cache")
	}
	ps = []projects.Project{deployTargets(projects.Deploy{Provider: "wrangler", Service: "edge"}, projects.Deploy{Provider: "wrangler", Service: "ghost"})}
	if sum := get(t, s, "wrangler"); byName(t, section(t, sum, "workers"), "ghost").Name != "ghost" {
		t.Fatal("a new deploy entry should be looked up at once")
	}
}

func TestDeploys(t *testing.T) {
	f := &fake{replies: map[string]reply{}}
	for k, v := range gcloudFake().replies {
		f.replies[k] = v
	}
	for k, v := range vercelFake().replies {
		f.replies[k] = v
	}
	f.replies["wrangler whoami --json"] = reply{fail: true, err: "You are not authenticated. Run wrangler login"}
	s := svc(f,
		projects.Project{ID: "shop", Deploy: []projects.Deploy{
			{Provider: "gcloud", Service: "api", Region: "us-central1"},
			{Provider: "gcloud", Service: "web", Region: "us-central1"}, // wrong region
			{Provider: "gcloud", Service: "api", Project: "other-proj"}, // another project
			{Provider: "vercel", Service: "api"},
			{Provider: "wrangler", Service: "edge"},
		}},
		projects.Project{ID: "plain"},
	)
	got := s.Deploys(context.Background(), false)
	if len(got["plain"]) != 0 || len(got["shop"]) != 5 {
		t.Fatalf("deploys = %+v", got)
	}
	d := got["shop"]
	if !d[0].Matched || d[0].Status != StatusReady || d[0].URL == "" || d[0].Updated == "" {
		t.Errorf("api = %+v", d[0])
	}
	if d[1].Matched || !strings.Contains(d[1].Note, "not found") {
		t.Errorf("wrong region should not match: %+v", d[1])
	}
	if d[2].Matched {
		t.Errorf("other project has no api: %+v", d[2])
	}
	if !d[3].Matched || d[3].Status != StatusFailed {
		t.Errorf("vercel api = %+v", d[3])
	}
	if d[4].Matched || d[4].Note != "Cloudflare: not signed in" {
		t.Errorf("wrangler = %+v", d[4])
	}
	if f.count("az ") != 0 || f.count("aws ") != 0 {
		t.Error("only the providers named by deploy entries should run")
	}
}

// secret strings every fake output carries in a field that must never leave.
var secrets = []string{
	"hunter2-secret-env", "tenant-secret", "AIDASECRET", "arn:aws", "secret-scope", "workers:write",
	"" + "AK" + "IA" + "ABCDEFGHIJKLMNOP" + "", "" + "sk_" + "li" + "ve_" + "abcdefghijklmnopqrstuvwxyz012345" + "",
}

func TestSecretsAreNeverReturned(t *testing.T) {
	f := &fake{replies: map[string]reply{}}
	for _, src := range []*fake{gcloudFake(), wranglerFake(), vercelFake()} {
		for k, v := range src.replies {
			f.replies[k] = v
		}
	}
	f.replies["az account show --output json"] = reply{out: `{"name":"Dev Subscription","user":{"name":"dev@example.com"},"tenantId":"tenant-secret"}`}
	f.replies["aws sts get-caller-identity --output json"] = reply{out: `{"UserId":"AIDASECRET","Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/dev"}`}
	// An error that echoes a token and an email must be scrubbed.
	f.replies["gcloud run services list --project other-proj --quiet --format=json"] = reply{fail: true,
		err: "ERROR: bad request for dev@example.com with key " + "AK" + "IA" + "ABCDEFGHIJKLMNOP" + " token " + "sk_" + "li" + "ve_" + "abcdefghijklmnopqrstuvwxyz012345" + ""}
	t.Setenv("LUCID_TEST_CLOUD_SECRET", "env-value-must-not-appear")

	s := svc(f, deployTargets(
		projects.Deploy{Provider: "wrangler", Service: "site"},
		projects.Deploy{Provider: "wrangler", Service: "edge"},
		projects.Deploy{Provider: "gcloud", Service: "api", Project: "other-proj"},
	))
	all, _ := json.Marshal(s.All(context.Background(), false))
	dep, _ := json.Marshal(s.Deploys(context.Background(), false))
	body := string(all) + string(dep)
	for _, secret := range append(secrets, "env-value-must-not-appear", "dev@example.com", "author_email", `"email"`) {
		// The one email allowed is the gcloud account id, which gcloud prints itself.
		check := strings.ReplaceAll(body, `"account":"dev@example.com"`, "")
		if strings.Contains(check, secret) {
			t.Errorf("response leaks %q", secret)
		}
	}
	if !strings.Contains(body, "u003cemail") || !strings.Contains(body, "u003credacted") {
		t.Errorf("expected scrubbed error text in %s", body)
	}
}

func TestHTTP(t *testing.T) {
	mux := http.NewServeMux()
	f := vercelFake()
	Register(mux, svc(f, projects.Project{ID: "shop", Deploy: []projects.Deploy{{Provider: "vercel", Service: "web"}}}))
	do := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code, rec.Body.String()
	}
	if code, body := do("/api/cloud/vercel"); code != 200 || !strings.Contains(body, `"account":"dev-user"`) {
		t.Errorf("vercel = %d %s", code, body)
	}
	if code, _ := do("/api/cloud/nope"); code != 404 {
		t.Errorf("unknown provider = %d", code)
	}
	code, body := do("/api/cloud/deploys")
	var dep struct {
		Projects map[string][]DeployStatus `json:"projects"`
	}
	if err := json.Unmarshal([]byte(body), &dep); err != nil || code != 200 || !dep.Projects["shop"][0].Matched {
		t.Errorf("deploys = %d %s", code, body)
	}
	before := f.count("vercel whoami")
	do("/api/cloud/vercel")
	if f.count("vercel whoami") != before {
		t.Error("cached read ran the CLI")
	}
	do("/api/cloud/vercel?refresh=1")
	if f.count("vercel whoami") != before+1 {
		t.Error("refresh=1 did not run the CLI")
	}
	code, body = do("/api/cloud")
	var all struct {
		Providers []Summary `json:"providers"`
	}
	if err := json.Unmarshal([]byte(body), &all); err != nil || code != 200 || len(all.Providers) != 5 || all.Providers[0].Provider != "gcloud" {
		t.Errorf("all = %d %s", code, body)
	}
}

// TestExecRunner runs the real runner against the test binary installed on
// PATH as a fake CLI.
func TestExecRunner(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "vercel"
	dst := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		dst += ".exe"
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("LUCID_FAKE_CLOUD", `{"username":"cli-user"}`)
	t.Setenv("LUCID_FAKE_CLOUD_ERR", "noise on stderr")

	s := New(func() []projects.Project { return nil })
	if sum := get(t, s, "vercel"); sum.State != StateConnected || sum.Account != "cli-user" {
		// project ls and ls print the same canned JSON, which decodes to empty lists.
		t.Fatalf("summary = %+v", sum)
	}
	// Providers whose CLI is not on PATH report so.
	if sum := get(t, s, "gcloud"); sum.State != StateNotInstalled {
		t.Errorf("gcloud = %+v", sum)
	}
	t.Setenv("LUCID_FAKE_CLOUD_FAIL", "1")
	t.Setenv("LUCID_FAKE_CLOUD_ERR", "Error: No existing credentials found. Please run `vercel login`")
	if sum, _ := s.Get(context.Background(), "vercel", true); sum.State != StateNotSignedIn {
		t.Errorf("failing CLI = %+v", sum)
	}
}
