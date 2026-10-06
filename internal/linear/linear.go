// Package linear reads Linear (teams, projects, the active cycle, issues) and
// creates issues from native board cards, through Linear's GraphQL API.
//
// The remote owns an issue: a card only keeps its identifier and URL, and
// nothing is synced back. The key is read from the environment variable named
// by integrations.linear.token when a request needs it, sent in a header, and
// never logged, stored or returned.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// MaxPages bounds how many pages of issues one listing follows.
const MaxPages = 4

const pageSize = 50

// Service talks to Linear. Build it with New; tests set the fields directly.
type Service struct {
	API    string
	Token  config.SecretRef
	Getenv func(string) string
	Doer   extapi.Doer
	Cache  extapi.Cache
	// Open opens the vault for the promote and link routes.
	Open memory.Opener
	// MCP reports whether a Linear MCP server is configured in any AI client.
	MCP func() bool
}

// New returns a Service for the real environment.
func New(c config.LinearConfig, open memory.Opener, mcp func() bool) *Service {
	s := &Service{API: c.APIURL, Token: c.Token, Getenv: os.Getenv, Open: open, MCP: mcp}
	s.Doer.RateLimited = rateLimited
	s.Doer.Classify = classify
	return s
}

// Status says whether the connector can work, without calling Linear.
type Status struct {
	// Configured is true when the variable named by the token setting is set.
	Configured bool `json:"configured"`
	// TokenRef is the reference (env:NAME), never the value.
	TokenRef string `json:"token_ref"`
	// MCP is true when a Linear MCP server is configured in an AI client. The
	// connector cannot use it: it needs an API key.
	MCP bool `json:"mcp"`
}

// Status reports the credential state.
func (s *Service) Status() Status {
	st := Status{TokenRef: s.Token.String()}
	st.Configured = s.token() != ""
	if s.MCP != nil {
		st.MCP = s.MCP()
	}
	return st
}

func (s *Service) token() string {
	if s.Getenv == nil || s.Token == "" {
		return ""
	}
	return strings.TrimSpace(s.Token.Resolve(s.Getenv))
}

// ---- wire types ----

// State is a workflow state: a board column.
type State struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"` // triage, backlog, unstarted, started, completed, canceled
	Color    string  `json:"color,omitempty"`
	Position float64 `json:"position"`
}

// Person is a user.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Team is a Linear team with its workflow states and active cycle.
type Team struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	States      []State `json:"states"`
	ActiveCycle *Cycle  `json:"active_cycle,omitempty"`
}

// Project is a Linear project.
type Project struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	State   string   `json:"state,omitempty"`
	TeamIDs []string `json:"team_ids"`
}

// Cycle is a team's active cycle.
type Cycle struct {
	ID       string  `json:"id"`
	Number   int     `json:"number"`
	Name     string  `json:"name,omitempty"`
	StartsAt string  `json:"starts_at"`
	EndsAt   string  `json:"ends_at"`
	Progress float64 `json:"progress"`
}

// Label is an issue label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Issue is one issue.
type Issue struct {
	ID            string  `json:"id"`
	Identifier    string  `json:"identifier"`
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Priority      int     `json:"priority"`
	PriorityLabel string  `json:"priority_label,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	State         State   `json:"state"`
	TeamID        string  `json:"team_id"`
	TeamKey       string  `json:"team_key"`
	ProjectID     string  `json:"project_id,omitempty"`
	ProjectName   string  `json:"project_name,omitempty"`
	Assignee      *Person `json:"assignee,omitempty"`
	Labels        []Label `json:"labels"`
}

// Meta is what the filters and the promote dialog need.
type Meta struct {
	Viewer   Person    `json:"viewer"`
	Teams    []Team    `json:"teams"`
	Projects []Project `json:"projects"`
}

// Filter narrows an issue listing.
type Filter struct {
	Team, Project string
	Me            bool
	// Done includes completed and canceled issues.
	Done bool
}

// IssueList is a listing. Truncated says more issues exist than were read.
type IssueList struct {
	Issues    []Issue `json:"issues"`
	Truncated bool    `json:"truncated"`
}

// ---- GraphQL ----

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Type string `json:"type"`
		Code string `json:"code"`
	} `json:"extensions"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

func isRateLimitErr(e gqlError) bool {
	return strings.EqualFold(e.Extensions.Code, "RATELIMITED") || strings.Contains(strings.ToLower(e.Extensions.Type), "ratelimit")
}

// rateLimited recognises Linear's rate limit answer, which is an error object
// (HTTP 400 or 200) rather than a 429.
func rateLimited(status int, body []byte) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status != http.StatusBadRequest && status != http.StatusOK {
		return false
	}
	var r gqlResponse
	if json.Unmarshal(body, &r) != nil {
		return false
	}
	for _, e := range r.Errors {
		if isRateLimitErr(e) {
			return true
		}
	}
	return false
}

// classify reads Linear's error object on an HTTP error answer, for the cases
// where the status alone is not enough (an authentication failure can arrive
// as 400).
func classify(status int, body []byte) error {
	var r gqlResponse
	if json.Unmarshal(body, &r) != nil || len(r.Errors) == 0 {
		return nil
	}
	return gqlErr(r.Errors[0])
}

func gqlErr(e gqlError) error {
	switch t := strings.ToLower(e.Extensions.Type + " " + e.Extensions.Code); {
	case strings.Contains(t, "authentication"), strings.Contains(t, "forbidden"):
		return extapi.ErrUnauthorized
	case isRateLimitErr(e):
		return extapi.ErrRateLimited
	case strings.Contains(strings.ToLower(e.Message), "not found"):
		return extapi.ErrNotFound
	}
	return extapi.ErrBadRequest
}

// query runs a GraphQL operation and decodes its data into out.
func (s *Service) query(ctx context.Context, q string, vars map[string]any, out any) error {
	tok := s.token()
	if tok == "" {
		return extapi.ErrNotConfigured
	}
	payload, err := json.Marshal(map[string]any{"query": q, "variables": vars})
	if err != nil {
		return extapi.ErrBadRequest
	}
	body, err := s.Doer.Do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, s.API, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		// A personal API key goes in the header as it is, without "Bearer".
		req.Header.Set("Authorization", tok)
		return req, nil
	})
	if err != nil {
		return err
	}
	var r gqlResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return extapi.ErrRemote
	}
	if len(r.Errors) > 0 {
		return gqlErr(r.Errors[0])
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(r.Data, out); err != nil {
		return extapi.ErrRemote
	}
	return nil
}

const stateFields = `id name type color position`

const metaQuery = `query {
  viewer { id name }
  teams(first: 50) { nodes { id key name
    activeCycle { id number name startsAt endsAt progress }
    states(first: 30) { nodes { ` + stateFields + ` } } } }
  projects(first: 100) { nodes { id name state teams(first: 10) { nodes { id } } } }
}`

type nodes[T any] struct {
	Nodes []T `json:"nodes"`
}

// Meta returns the viewer, teams (with workflow states and active cycle) and
// projects. It is cached for a minute.
func (s *Service) Meta(ctx context.Context) (*Meta, error) {
	if v, ok := s.Cache.Get("meta"); ok {
		return v.(*Meta), nil
	}
	var d struct {
		Viewer Person `json:"viewer"`
		Teams  nodes[struct {
			ID          string       `json:"id"`
			Key         string       `json:"key"`
			Name        string       `json:"name"`
			ActiveCycle *Cycle       `json:"activeCycle"`
			States      nodes[State] `json:"states"`
		}] `json:"teams"`
		Projects nodes[struct {
			ID    string                     `json:"id"`
			Name  string                     `json:"name"`
			State string                     `json:"state"`
			Teams nodes[struct{ ID string }] `json:"teams"`
		}] `json:"projects"`
	}
	if err := s.query(ctx, metaQuery, nil, &d); err != nil {
		return nil, err
	}
	m := &Meta{Viewer: d.Viewer, Teams: []Team{}, Projects: []Project{}}
	for _, t := range d.Teams.Nodes {
		states := t.States.Nodes
		if states == nil {
			states = []State{}
		}
		m.Teams = append(m.Teams, Team{ID: t.ID, Key: t.Key, Name: t.Name, States: states, ActiveCycle: t.ActiveCycle})
	}
	for _, p := range d.Projects.Nodes {
		ids := []string{}
		for _, t := range p.Teams.Nodes {
			ids = append(ids, t.ID)
		}
		m.Projects = append(m.Projects, Project{ID: p.ID, Name: p.Name, State: p.State, TeamIDs: ids})
	}
	s.Cache.Put("meta", m)
	return m, nil
}

const issueFields = `id identifier title url priority priorityLabel updatedAt
  state { ` + stateFields + ` } team { id key } project { id name } assignee { id name }
  labels(first: 10) { nodes { name color } }`

const issuesQuery = `query($filter: IssueFilter, $after: String) {
  issues(filter: $filter, first: ` + "50" + `, after: $after, orderBy: updatedAt) {
    nodes { ` + issueFields + ` }
    pageInfo { hasNextPage endCursor }
  }
}`

type rawIssue struct {
	ID            string `json:"id"`
	Identifier    string `json:"identifier"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Priority      int    `json:"priority"`
	PriorityLabel string `json:"priorityLabel"`
	UpdatedAt     string `json:"updatedAt"`
	State         State  `json:"state"`
	Team          struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"team"`
	Project *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	Assignee *Person      `json:"assignee"`
	Labels   nodes[Label] `json:"labels"`
}

func (r rawIssue) issue() Issue {
	i := Issue{
		ID: r.ID, Identifier: r.Identifier, Title: r.Title, URL: r.URL, Priority: r.Priority,
		PriorityLabel: r.PriorityLabel, UpdatedAt: r.UpdatedAt, State: r.State,
		TeamID: r.Team.ID, TeamKey: r.Team.Key, Assignee: r.Assignee, Labels: r.Labels.Nodes,
	}
	if i.Labels == nil {
		i.Labels = []Label{}
	}
	if r.Project != nil {
		i.ProjectID, i.ProjectName = r.Project.ID, r.Project.Name
	}
	return i
}

func (f Filter) gql() map[string]any {
	m := map[string]any{}
	if f.Team != "" {
		m["team"] = map[string]any{"id": map[string]any{"eq": f.Team}}
	}
	if f.Project != "" {
		m["project"] = map[string]any{"id": map[string]any{"eq": f.Project}}
	}
	if f.Me {
		m["assignee"] = map[string]any{"isMe": map[string]any{"eq": true}}
	}
	if !f.Done {
		m["state"] = map[string]any{"type": map[string]any{"nin": []string{"completed", "canceled"}}}
	}
	return m
}

// Issues lists issues, following pages up to MaxPages. Cached for a minute.
func (s *Service) Issues(ctx context.Context, f Filter) (*IssueList, error) {
	key := fmt.Sprintf("issues|%s|%s|%t|%t", f.Team, f.Project, f.Me, f.Done)
	if v, ok := s.Cache.Get(key); ok {
		return v.(*IssueList), nil
	}
	out := &IssueList{Issues: []Issue{}}
	var after any
	for page := 0; page < MaxPages; page++ {
		var d struct {
			Issues struct {
				Nodes    []rawIssue `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issues"`
		}
		if err := s.query(ctx, issuesQuery, map[string]any{"filter": f.gql(), "after": after}, &d); err != nil {
			return nil, err
		}
		for _, r := range d.Issues.Nodes {
			out.Issues = append(out.Issues, r.issue())
		}
		if !d.Issues.PageInfo.HasNextPage || d.Issues.PageInfo.EndCursor == "" {
			break
		}
		if page == MaxPages-1 {
			out.Truncated = true
		}
		after = d.Issues.PageInfo.EndCursor
	}
	s.Cache.Put(key, out)
	return out, nil
}

var (
	identRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,9}-[0-9]{1,9}$`)
	urlRE   = regexp.MustCompile(`/issue/([A-Za-z][A-Za-z0-9]{0,9}-[0-9]{1,9})(?:/|$)`)
)

// ErrBadIdentifier means the text is neither an identifier nor an issue URL.
var ErrBadIdentifier = errors.New("not a Linear issue identifier (like ENG-12) or issue URL")

// ParseIdentifier takes "ENG-12" or a Linear issue URL and returns the
// upper-case identifier.
func ParseIdentifier(s string) (string, error) {
	s = strings.TrimSpace(s)
	if identRE.MatchString(s) {
		return strings.ToUpper(s), nil
	}
	if u, err := url.Parse(s); err == nil && u.Host == "linear.app" {
		if m := urlRE.FindStringSubmatch(u.Path); m != nil {
			return strings.ToUpper(m[1]), nil
		}
	}
	return "", ErrBadIdentifier
}

// Issue fetches one issue by identifier.
func (s *Service) Issue(ctx context.Context, identifier string) (*Issue, error) {
	id, err := ParseIdentifier(identifier)
	if err != nil {
		return nil, extapi.ErrBadRequest
	}
	var d struct {
		Issue *rawIssue `json:"issue"`
	}
	if err := s.query(ctx, `query($id: String!) { issue(id: $id) { `+issueFields+` } }`, map[string]any{"id": id}, &d); err != nil {
		return nil, err
	}
	if d.Issue == nil {
		return nil, extapi.ErrNotFound
	}
	i := d.Issue.issue()
	return &i, nil
}

// NewIssue is the input of CreateIssue.
type NewIssue struct {
	Team        string `json:"team"`
	Project     string `json:"project,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// CreateIssue creates an issue and drops the cached reads.
func (s *Service) CreateIssue(ctx context.Context, in NewIssue) (*Issue, error) {
	if strings.TrimSpace(in.Title) == "" || in.Team == "" {
		return nil, extapi.ErrBadRequest
	}
	input := map[string]any{"teamId": in.Team, "title": strings.TrimSpace(in.Title)}
	if in.Project != "" {
		input["projectId"] = in.Project
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	var d struct {
		IssueCreate struct {
			Success bool      `json:"success"`
			Issue   *rawIssue `json:"issue"`
		} `json:"issueCreate"`
	}
	q := `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { ` + issueFields + ` } } }`
	if err := s.query(ctx, q, map[string]any{"input": input}, &d); err != nil {
		return nil, err
	}
	if !d.IssueCreate.Success || d.IssueCreate.Issue == nil {
		return nil, extapi.ErrRemote
	}
	s.Cache.Clear()
	i := d.IssueCreate.Issue.issue()
	return &i, nil
}
