package sections

// Templates are the built-in sections a user can add with one click. Each
// is valid (a test checks it); adding one copies it into the user's
// sections folder, where it can be changed or removed like any other.
var Templates = []Section{
	{
		ID: "prs-waiting", Title: "PRs waiting for me", Description: "Draft and open PRs from Work sessions, not yet merged.",
		Source: Source{API: "/api/work/sessions"}, View: "list",
		Filter: []Filter{{Field: "pr_state", Op: "in", Value: []any{"draft", "open"}}},
		Sort:   &Sort{Field: "started", Desc: true},
		Fields: []Field{{Path: "title"}, {Path: "pr_state", Format: "badge"}, {Path: "project"}, {Path: "pr_url", Label: "PR", Format: "link"}},
	},
	{
		ID: "spend-week", Title: "This week's spend", Description: "What Lucidbench's own agent runs cost in the last 7 days, per provider.",
		Source: Source{API: "/api/usage/summary", Params: map[string]string{"days": "7"}}, View: "bars", Rows: "lucidbench.providers",
		Sort:   &Sort{Field: "cost_usd", Desc: true},
		Fields: []Field{{Path: "name"}, {Path: "cost_usd", Format: "usd"}},
	},
	{
		ID: "cards-due", Title: "Cards due soon", Description: "Cards on the work board due in the next 7 days, or overdue.",
		Source: Source{API: "/api/boards/{board}", Params: map[string]string{"board": "work"}}, View: "table", Rows: "cards",
		Filter: []Filter{{Field: "done", Op: "ne", Value: true}, {Field: "due", Op: "within_days", Value: float64(7)}},
		Sort:   &Sort{Field: "due"},
		Fields: []Field{{Path: "title", Label: "Card"}, {Path: "column", Label: "Column", Format: "badge"}, {Path: "due", Label: "Due", Format: "date"}},
	},
	{
		ID: "failing-deploys", Title: "Failing deploys", Description: "Deploys whose cloud status is failed.",
		Source: Source{API: "/api/cloud/deploys"}, View: "list", Rows: "projects.*",
		Filter: []Filter{{Field: "status", Op: "eq", Value: "failed"}},
		Fields: []Field{{Path: "service"}, {Path: "_key", Label: "project"}, {Path: "provider", Format: "badge"}, {Path: "url", Label: "open", Format: "link"}},
	},
	{
		ID: "agents-working", Title: "Agents working", Description: "Work sessions running now.",
		Source: Source{API: "/api/work/sessions"}, View: "stat",
		Filter: []Filter{{Field: "status", Op: "eq", Value: "running"}},
		Fields: []Field{{Path: "id", Label: "running", Agg: "count"}},
	},
	{
		ID: "project-sessions", Title: "Sessions on this project", Description: "The latest Work sessions on the project, with their cost.",
		Placement: PlaceProject,
		Source:    Source{API: "/api/work/sessions"}, View: "table",
		Filter: []Filter{{Field: "project", Op: "eq", Value: ProjectToken}},
		Sort:   &Sort{Field: "started", Desc: true}, Limit: 5,
		Fields: []Field{{Path: "title", Label: "Session"}, {Path: "status", Format: "badge"}, {Path: "started", Format: "relative"}, {Path: "usage.cost_usd", Label: "Cost", Format: "usd"}},
	},
}

// Template returns a built-in template by id, normalised.
func Template(id string) (Section, bool) {
	for _, t := range Templates {
		if t.ID == id {
			return Normalise(t), true
		}
	}
	return Section{}, false
}
