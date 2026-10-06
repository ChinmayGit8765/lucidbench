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
		if n := len(k.Questions); n < 5 || n > 7 {
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
