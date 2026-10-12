package assess

import (
	"slices"
	"strings"
	"testing"
)

func TestKindsLoadAndValidate(t *testing.T) {
	want := []string{"game", "web-app", "desktop-app", "cli-library", "service-api", "ml-research", "site", "video-media", "coursework"}
	var got []string
	for _, k := range Kinds() {
		got = append(got, k.ID)
		if n := len(k.Questions); n < MinQuestions || n > MaxQuestions {
			t.Errorf("%s has %d questions", k.ID, n)
		}
		if len(k.Rules) == 0 {
			t.Errorf("%s has no rules", k.ID)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
}

// Every rule of every kind must be reachable: for each rule, some answer set
// makes it apply, so a typo in a rule is a test failure and not a silent miss.
func TestEveryRuleCanApply(t *testing.T) {
	for _, k := range Kinds() {
		qs := map[string]Question{}
		for _, q := range k.Questions {
			qs[q.ID] = q
		}
		for i, r := range k.Rules {
			answers := map[string]string{}
			for qid, vals := range r.When {
				v := vals[0]
				if v == "*" {
					v = "x"
				}
				answers[qid] = v
			}
			if !r.applies(answers) {
				t.Errorf("%s rule %d never applies", k.ID, i+1)
			}
		}
	}
}

func TestBadKindsAreRejected(t *testing.T) {
	base := `id: x
name: X
blurb: b
questions:
  - {id: a, text: A, type: bool}
  - {id: b, text: B, type: bool}
  - {id: c, text: C, type: bool}
  - {id: d, text: D, type: bool}
  - {id: e, text: E, type: choice, options: [p, q]}
`
	if _, err := parseKind([]byte(base)); err != nil {
		t.Fatalf("base kind: %v", err)
	}
	cases := map[string]string{
		"unknown question":  base + "rules:\n  - when: {zzz: x}\n    criteria: [c]\n",
		"unknown option":    base + "rules:\n  - when: {e: r}\n    criteria: [c]\n",
		"bad bool":          base + "rules:\n  - when: {a: yes}\n    criteria: [c]\n",
		"bad level":         base + "rules:\n  - risk: {level: severe, reason: r}\n",
		"risk needs reason": base + "rules:\n  - risk: {level: low}\n",
		"unknown field":     base + "surprise: 1\n",
		"too few questions": strings.Replace(base, "  - {id: d, text: D, type: bool}\n", "", 1),
		"too many questions": strings.Replace(base, "questions:\n",
			"questions:\n  - {id: f, text: F, type: bool}\n  - {id: g, text: G, type: bool}\n  - {id: h, text: H, type: bool}\n  - {id: i, text: I, type: bool}\n  - {id: j, text: J, type: bool}\n  - {id: k, text: K, type: bool}\n", 1),
		"optional text has options": strings.Replace(base, "{id: a, text: A, type: bool}", "{id: a, text: A, type: text, options: [x, y]}", 1),
	}
	for name, src := range cases {
		if _, err := parseKind([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Assessment{Kind: "game", Answers: map[string]string{
		"engine": "godot", "stage": "prototype", "audience": "just-me", "multiplayer": "false", "saves": "true",
	}}
	if err := Validate(ok); err != nil {
		t.Fatalf("valid assessment: %v", err)
	}
	bad := func(name string, mut func(a *Assessment), want string) {
		t.Helper()
		a := Assessment{Kind: ok.Kind, Answers: map[string]string{}}
		for k, v := range ok.Answers {
			a.Answers[k] = v
		}
		mut(&a)
		err := Validate(a)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, want)
		}
	}
	bad("kind", func(a *Assessment) { a.Kind = "spaceship" }, "unknown kind")
	bad("option", func(a *Assessment) { a.Answers["engine"] = "cryengine" }, "answers.engine")
	bad("missing", func(a *Assessment) { delete(a.Answers, "stage") }, "answers.stage: required")
	bad("bool", func(a *Assessment) { a.Answers["saves"] = "maybe" }, "answers.saves")
	bad("stray", func(a *Assessment) { a.Answers["nope"] = "x" }, "answers.nope")
	bad("long text", func(a *Assessment) { a.Answers["platforms"] = strings.Repeat("x", MaxText+1) }, "answers.platforms")
}

func TestSuggestMapsAnswers(t *testing.T) {
	// A multiplayer Godot game for the public: engine command, save and
	// network criteria, the highest risk wins and every reason is kept.
	s := Suggest(Assessment{Kind: "game", Answers: map[string]string{
		"engine": "godot", "stage": "vertical-slice", "audience": "public-release", "multiplayer": "true", "saves": "false",
	}})
	if !slices.Contains(s.CheckCommands, "godot") {
		t.Errorf("commands = %v", s.CheckCommands)
	}
	if s.Risk.Level != "high" || len(s.Risk.Reasons) != 2 {
		t.Errorf("risk = %+v", s.Risk)
	}
	joined := strings.Join(s.DoneCriteria, "\n")
	for _, want := range []string{"without desync", "clean install", "headless", "play session"} {
		if !strings.Contains(joined, want) {
			t.Errorf("criteria missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "restores the same state") {
		t.Errorf("save criterion applied when saves is false")
	}
	for _, c := range s.DoneCriteria {
		if !strings.Contains(c, "proof:") {
			t.Errorf("criterion without a proof hint: %q", c)
		}
	}

	// A private prototype stays low.
	s = Suggest(Assessment{Kind: "game", Answers: map[string]string{
		"engine": "unity", "stage": "prototype", "audience": "just-me", "multiplayer": "false", "saves": "false",
	}})
	if s.Risk.Level != "low" || len(s.CheckCommands) != 0 {
		t.Errorf("prototype: risk %+v commands %v", s.Risk, s.CheckCommands)
	}
	if !strings.Contains(strings.Join(s.DoneCriteria, "\n"), "<engine test command>") {
		t.Errorf("no engine placeholder: %v", s.DoneCriteria)
	}

	// A Go service with outside callers and money at stake.
	s = Suggest(Assessment{Kind: "service-api", Answers: map[string]string{
		"language": "go", "callers": "outside-customers", "store": "true", "auth": "true", "uptime": "money-is-lost", "deploy": "managed-cloud",
	}})
	if !slices.Equal(s.CheckCommands, []string{"go test", "go vet", "gofmt"}) || s.Risk.Level != "high" {
		t.Errorf("service: commands %v risk %+v", s.CheckCommands, s.Risk)
	}

	// An unknown kind suggests nothing.
	if s := Suggest(Assessment{Kind: "nope"}); len(s.DoneCriteria) != 0 || s.Risk.Level != "low" {
		t.Errorf("unknown kind: %+v", s)
	}
}

func TestKindForType(t *testing.T) {
	for typ, want := range map[string]string{"game": "game", "web-app": "web-app", "cli": "cli-library", "library": "cli-library", "service": "service-api", "video-system": "video-media", "site": "site", "ml-research": "ml-research", "desktop-app": "desktop-app", "": ""} {
		if got := KindForType(typ); got != want {
			t.Errorf("KindForType(%q) = %q, want %q", typ, got, want)
		}
	}
}

// The project types the preliminary questionnaire must cover: game, product
// (web/SaaS), video system, CLI or library, and research or experiment. Each
// loads with six to ten questions, and a full answer set validates and
// assesses into proof-carrying criteria and the expected risk.
func TestProjectTypeKindsAssess(t *testing.T) {
	cases := []struct {
		kind     string
		answers  map[string]string
		level    string
		criteria []string
	}{
		{"game", map[string]string{
			"engine": "godot", "stage": "prototype", "audience": "friends", "multiplayer": "false", "saves": "true", "platforms": "windows",
			"performance": "60-fps-or-more", "monetisation": "in-app-purchases", "content": "true",
		}, "high", []string{"frame rate holds", "sandbox purchase", "missing assets", "restores the same state"}},
		{"web-app", map[string]string{
			"users": "paying-customers", "accounts": "true", "payments": "false", "data": "false", "tests": "some", "deploy": "ci-pipeline",
			"tenancy": "many-customers-shared", "monitoring": "a-user-tells-me",
		}, "high", []string{"customer B's records", "deliberate test error", "rolled back"}},
		{"video-media", map[string]string{
			"role": "stream-or-play", "input": "my-own", "size": "minutes", "pipeline": "false",
			"latency": "live-under-seconds", "engine": "hardware-or-gpu-encoders", "reference-clips": "false",
		}, "high", []string{"30-minute run", "hardware encoding turned off", "awkward clips", "target player"}},
		{"cli-library", map[string]string{
			"shape": "command", "language": "go", "consumers": "just-me", "stable-api": "false", "published": "false", "docs": "readme",
			"platforms": "several", "writes-files": "true",
		}, "medium", []string{"CI matrix", "temporary home folder", "--help"}},
		{"ml-research", map[string]string{
			"goal": "learn-something", "data": "public-dataset", "compute": "laptop", "reproducible": "true", "baseline": "true",
			"threshold": "false", "timebox": "open-ended",
		}, "medium", []string{"thresholds are written down", "stop date"}},
	}
	for _, c := range cases {
		k, ok := Get(c.kind)
		if !ok {
			t.Fatalf("kind %s missing", c.kind)
		}
		if n := len(k.Questions); n < 6 || n > MaxQuestions {
			t.Errorf("%s has %d questions, want 6 to %d", c.kind, n, MaxQuestions)
		}
		a := Assessment{Kind: c.kind, Answers: c.answers}
		if err := Validate(a); err != nil {
			t.Errorf("%s: %v", c.kind, err)
			continue
		}
		s := Suggest(a)
		if s.Risk.Level != c.level || len(s.Risk.Reasons) == 0 {
			t.Errorf("%s risk = %+v, want %s", c.kind, s.Risk, c.level)
		}
		joined := strings.Join(s.DoneCriteria, "\n")
		for _, want := range c.criteria {
			if !strings.Contains(joined, want) {
				t.Errorf("%s criteria missing %q:\n%s", c.kind, want, joined)
			}
		}
		for _, cr := range s.DoneCriteria {
			if !strings.Contains(cr, "proof:") {
				t.Errorf("%s criterion without a proof hint: %q", c.kind, cr)
			}
		}
	}
}

// Questions added after a kind shipped are optional, so an assessment saved
// before them still validates; a wrong answer to one is still refused.
func TestOptionalQuestionsKeepOldAssessmentsValid(t *testing.T) {
	old := Assessment{Kind: "game", Answers: map[string]string{
		"engine": "godot", "stage": "prototype", "audience": "just-me", "multiplayer": "false", "saves": "false",
	}}
	if err := Validate(old); err != nil {
		t.Fatalf("assessment saved before the new questions: %v", err)
	}
	if s := Suggest(old); s.Risk.Level != "low" {
		t.Errorf("unanswered optional questions changed the risk: %+v", s.Risk)
	}
	old.Answers["performance"] = "120-fps"
	if err := Validate(old); err == nil || !strings.Contains(err.Error(), "answers.performance") {
		t.Errorf("bad optional answer: %v", err)
	}
	old.Answers["performance"] = ""
	old.Answers["content"] = "maybe"
	if err := Validate(old); err == nil || !strings.Contains(err.Error(), "answers.content") {
		t.Errorf("bad optional bool: %v", err)
	}
	// Required questions stay required.
	delete(old.Answers, "content")
	delete(old.Answers, "engine")
	if err := Validate(old); err == nil || !strings.Contains(err.Error(), "answers.engine: required") {
		t.Errorf("required question: %v", err)
	}
}
