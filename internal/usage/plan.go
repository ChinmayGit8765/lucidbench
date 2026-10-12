package usage

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Planning works out, from local records only, how fast each provider
// account is using its current limit window, when that window would run out
// at that pace, and which of the things the user plans to do today fit
// before each reset. Nothing here calls a provider.
//
// Tokens here are input, output and cache writes. Cache reads are left out:
// they are large, cheap and say little about how close a limit is.

// Plan kinds: what a planned item would run.
const (
	PlanCouncil   = "council"   // a council: proposer and critics
	PlanWork      = "work"      // one Work turn
	PlanSection   = "section"   // "Describe a section"
	PlanRank      = "rank"      // "Ask an agent to rank" on Next up
	PlanAssistant = "assistant" // one Assistant turn or braindump parse
)

// PlanKinds lists the kinds in display order.
var PlanKinds = []string{PlanCouncil, PlanWork, PlanSection, PlanRank, PlanAssistant}

// planSource maps a kind to the source its records are counted under.
var planSource = map[string]string{
	PlanCouncil: "council", PlanWork: "work", PlanSection: "sections", PlanRank: "nextup", PlanAssistant: "assistant",
}

// planRole is the team role that runs a kind; "" means no role.
var planRole = map[string]string{PlanCouncil: "proposer", PlanWork: "builder", PlanRank: "scout"}

// PlanProviders are the providers a planned item may be suggested for.
var PlanProviders = []string{"claude", "codex", "grok"}

// Planning limits.
const (
	DefaultPaceHours = 2
	MaxPaceHours     = 24
	// EstimateRuns is how many recent runs an estimate is the median of.
	EstimateRuns = 20
	// claudeBlock is Claude Code's session window, estimated from its logs.
	claudeBlock = 5 * time.Hour
	// claudeHistory is how far back Claude's busiest block is looked for.
	claudeHistory = 30 * 24 * time.Hour
)

// Pace is one provider account's current limit window and how fast it is
// being used.
type Pace struct {
	Provider string `json:"provider"`
	Account  string `json:"account"`
	Window   string `json:"window"` // "5h", "weekly", …
	// Basis is "reported" when the CLI wrote the window (Codex) and
	// "estimated" when it is worked out from the logs (Claude's 5-hour block,
	// measured against the busiest block lately).
	Basis    string     `json:"basis"`
	StartsAt *time.Time `json:"starts_at,omitempty"`
	ResetsAt *time.Time `json:"resets_at,omitempty"`
	// UsedTokens are spent since the window started.
	UsedTokens int64 `json:"used_tokens"`
	// CapacityTokens is the window's size in tokens, 0 when unknown.
	CapacityTokens int64    `json:"capacity_tokens,omitempty"`
	UsedPercent    *float64 `json:"used_percent"`
	// BurnPerHour is tokens per hour over the last Hours.
	BurnPerHour float64 `json:"burn_per_hour"`
	Hours       float64 `json:"hours"`
	// RunsOutAt is when the window is used up at this pace, if before reset.
	RunsOutAt *time.Time `json:"runs_out_at,omitempty"`
	// Verdict is runs_out, lasts, idle or unknown.
	Verdict string `json:"verdict"`
	Summary string `json:"summary"`
}

// remaining is what is left of the window now, or -1 when unknown.
func (p Pace) remaining() int64 {
	if p.CapacityTokens <= 0 {
		return -1
	}
	if r := p.CapacityTokens - p.UsedTokens; r > 0 {
		return r
	}
	return 0
}

func weight(s sample) int64 { return s.In + s.Out + s.CW }

// tokensBetween sums the samples with from <= TS < to.
func tokensBetween(ss []sample, from, to time.Time) int64 {
	var n int64
	for _, s := range ss {
		if s.TS >= from.Unix() && s.TS < to.Unix() {
			n += weight(s)
		}
	}
	return n
}

// burnRate is tokens per hour over the hours before now.
func burnRate(ss []sample, now time.Time, hours float64) float64 {
	if hours <= 0 {
		return 0
	}
	from := now.Add(-time.Duration(hours * float64(time.Hour)))
	return float64(tokensBetween(ss, from, now.Add(time.Second))) / hours
}

// clock formats a time as the user reads it: 14:20.
func clock(t time.Time, loc *time.Location) string { return t.In(loc).Format("15:04") }

// span is "40 min", "2 h 10 min".
func span(d time.Duration) string {
	m := int(math.Round(d.Minutes()))
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

func windowName(label string) string {
	switch label {
	case "weekly":
		return "weekly limit"
	case "":
		return "limit window"
	}
	return label + " limit"
}

// project fills in the projection of a pace whose window, used tokens,
// capacity and burn rate are known.
func (p *Pace) project(now time.Time, loc *time.Location) {
	who := p.Provider
	if p.Account != "" && p.Account != "default" {
		who += " (" + p.Account + ")"
	}
	limit := windowName(p.Window)
	resets := ""
	if p.ResetsAt != nil {
		resets = " at " + clock(*p.ResetsAt, loc)
	}
	if p.CapacityTokens > 0 {
		pct := math.Min(100, float64(p.UsedTokens)/float64(p.CapacityTokens)*100)
		pct = math.Round(pct*10) / 10
		p.UsedPercent = &pct
	}
	rem := p.remaining()
	switch {
	case rem < 0:
		p.Verdict = "unknown"
		if p.BurnPerHour > 0 {
			p.Summary = fmt.Sprintf("%s is using about %s tokens an hour; the size of its %s is not known yet, so there is no projection.", who, compact(int64(p.BurnPerHour)), limit)
		} else {
			p.Summary = fmt.Sprintf("%s has not been used in the last %s; the size of its %s is not known yet.", who, span(time.Duration(p.Hours*float64(time.Hour))), limit)
		}
	case rem == 0:
		p.Verdict = "runs_out"
		t := now
		p.RunsOutAt = &t
		p.Summary = fmt.Sprintf("%s's %s is used up; it resets%s.", who, limit, resets)
	case p.BurnPerHour <= 0:
		p.Verdict = "idle"
		p.Summary = fmt.Sprintf("No %s use in the last %s, so the %s holds until it resets%s.", who, span(time.Duration(p.Hours*float64(time.Hour))), limit, resets)
	default:
		out := now.Add(time.Duration(float64(rem) / p.BurnPerHour * float64(time.Hour))).Round(time.Minute)
		if p.ResetsAt != nil && !out.Before(*p.ResetsAt) {
			p.Verdict = "lasts"
			p.Summary = fmt.Sprintf("At this pace %s's %s lasts until it resets%s.", who, limit, resets)
			return
		}
		p.Verdict = "runs_out"
		p.RunsOutAt = &out
		if p.ResetsAt != nil {
			p.Summary = fmt.Sprintf("At this pace %s hits its %s at %s, %s before it resets.", who, limit, clock(out, loc), span(p.ResetsAt.Sub(out)))
		} else {
			p.Summary = fmt.Sprintf("At this pace %s hits its %s at %s.", who, limit, clock(out, loc))
		}
	}
}

// compact is 1.2M, 34k, 980.
func compact(n int64) string {
	a := math.Abs(float64(n))
	switch {
	case a >= 1e6:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case a >= 1e4:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case a >= 1e3:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "k"
	}
	return fmt.Sprint(n)
}

// codexPace is the pace of one reported Codex window. The window's size in
// tokens is implied by the share it says is used: if T tokens were spent
// since it started when it read U %, it holds about T / U.
func codexPace(account, label string, w *cxWin, observed time.Time, ss []sample, now time.Time, hours float64, loc *time.Location) *Pace {
	if w == nil || w.Resets == nil || w.Minutes == nil || *w.Minutes <= 0 {
		return nil
	}
	resets := time.Unix(*w.Resets, 0).UTC()
	if !resets.After(now) {
		return nil // reset since it was seen: nothing current to project
	}
	start := resets.Add(-time.Duration(*w.Minutes) * time.Minute)
	p := &Pace{Provider: "codex", Account: account, Window: label, Basis: "reported", StartsAt: &start, ResetsAt: &resets, Hours: hours}
	p.UsedTokens = tokensBetween(ss, start, now.Add(time.Second))
	p.BurnPerHour = burnRate(ss, now, hours)
	if w.Used != nil && *w.Used >= 1 {
		if seen := tokensBetween(ss, start, observed.Add(time.Second)); seen > 0 {
			p.CapacityTokens = int64(float64(seen) / (*w.Used / 100))
		}
	}
	p.project(now, loc)
	if p.CapacityTokens == 0 && w.Used != nil {
		u := *w.Used
		p.UsedPercent = &u
	}
	return p
}

// claudeBlocks splits samples into Claude Code's 5-hour blocks: a block
// starts at the hour of the first message after the previous block ended.
func claudeBlocks(ss []sample) (starts []time.Time, totals []int64) {
	sorted := append([]sample(nil), ss...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TS < sorted[j].TS })
	for _, s := range sorted {
		t := time.Unix(s.TS, 0).UTC()
		if len(starts) == 0 || !t.Before(starts[len(starts)-1].Add(claudeBlock)) {
			starts = append(starts, t.Truncate(time.Hour))
			totals = append(totals, 0)
		}
		totals[len(totals)-1] += weight(s)
	}
	return starts, totals
}

// claudePace is Claude's current 5-hour block, estimated from its logs. Its
// size is the busiest finished block of the last 30 days: a floor for the
// real limit, which Claude Code does not write down.
func claudePace(account string, ss []sample, now time.Time, hours float64, loc *time.Location) *Pace {
	var recent []sample
	for _, s := range ss {
		if s.TS >= now.Add(-claudeHistory).Unix() && s.TS <= now.Unix() {
			recent = append(recent, s)
		}
	}
	if len(recent) == 0 {
		return nil
	}
	starts, totals := claudeBlocks(recent)
	p := &Pace{Provider: "claude", Account: account, Window: "5h", Basis: "estimated", Hours: hours}
	last := len(starts) - 1
	var busiest int64
	end := len(starts)
	if now.Before(starts[last].Add(claudeBlock)) {
		st, rs := starts[last], starts[last].Add(claudeBlock)
		p.StartsAt, p.ResetsAt, p.UsedTokens = &st, &rs, totals[last]
		end = last
	}
	for _, t := range totals[:end] {
		busiest = max(busiest, t)
	}
	if busiest > p.UsedTokens {
		p.CapacityTokens = busiest
	}
	p.BurnPerHour = burnRate(recent, now, hours)
	if p.StartsAt == nil {
		// No block running: the next message starts a fresh one.
		p.Verdict = "idle"
		p.Summary = fmt.Sprintf("No Claude 5-hour block is running%s; the next message starts a fresh one.", acct(account))
		return p
	}
	p.project(now, loc)
	if p.CapacityTokens == 0 && busiest > 0 {
		p.Summary = fmt.Sprintf("This 5-hour block%s is already busier than any in the last 30 days, so there is no estimate of where its limit is.", acct(account))
	}
	return p
}

func acct(a string) string {
	if a == "" || a == "default" {
		return ""
	}
	return " (" + a + ")"
}

// paces works out every provider account's pace from the scanned logs.
func paces(sc scanned, now time.Time, hours float64, loc *time.Location) []Pace {
	out := []Pace{}
	byAcct := map[string][]sample{}
	var accts []string
	for _, s := range sc.claude {
		if _, ok := byAcct[s.Account]; !ok {
			accts = append(accts, s.Account)
		}
		byAcct[s.Account] = append(byAcct[s.Account], s)
	}
	sort.Strings(accts)
	for _, a := range accts {
		if p := claudePace(a, byAcct[a], now, hours, loc); p != nil {
			out = append(out, *p)
		}
	}
	cxByAcct := map[string][]codexHit{}
	accts = nil
	for _, e := range sc.codex {
		if _, ok := cxByAcct[e.Account]; !ok {
			accts = append(accts, e.Account)
		}
		cxByAcct[e.Account] = append(cxByAcct[e.Account], e)
	}
	sort.Strings(accts)
	for _, a := range accts {
		hits := cxByAcct[a]
		var latest *codexEntry
		for _, e := range hits {
			if (e.Primary != nil || e.Secondary != nil) && (latest == nil || e.RLTS > latest.RLTS) {
				latest = e.codexEntry
			}
		}
		if latest == nil {
			continue
		}
		ss := codexSamples(hits)
		observed := time.Unix(latest.RLTS, 0).UTC()
		for _, w := range []*cxWin{latest.Primary, latest.Secondary} {
			if w == nil {
				continue
			}
			if p := codexPace(a, windowLabel("", w.Minutes), w, observed, ss, now, hours, loc); p != nil {
				out = append(out, *p)
			}
		}
	}
	return out
}

// ---- estimates ----

// Estimate is what one planned item usually costs.
type Estimate struct {
	// Tokens is the median spent on the item's provider, without cache reads.
	Tokens int64 `json:"tokens"`
	// CostUSD is the median cost of the whole run, where the CLIs reported it.
	CostUSD float64 `json:"cost_usd,omitempty"`
	Runs    int     `json:"runs"`
	// Basis says what the estimate is from: model (same kind, provider and
	// model), provider (same kind and provider), kind (same kind, any
	// provider) or none.
	Basis string `json:"basis"`
}

// planRun is one past run of a kind: the tokens each provider spent in it.
type planRun struct {
	At     time.Time
	Tokens map[string]int64 // provider -> tokens
	Models map[string]bool  // "provider/model"
	Cost   float64
}

// planRuns groups own records into runs of each kind. A council is one run
// across its steps; a Work turn and every other call is one run each.
func planRuns(runs []ownRun) map[string][]planRun {
	out := map[string][]planRun{}
	councils := map[string]*planRun{}
	for _, r := range runs {
		kind := ""
		for k, src := range planSource {
			if src == r.Source {
				kind = k
			}
		}
		if kind == "" {
			continue
		}
		var pr *planRun
		if kind == PlanCouncil {
			pr = councils[r.File]
			if pr == nil {
				pr = &planRun{At: r.At, Tokens: map[string]int64{}, Models: map[string]bool{}}
				councils[r.File] = pr
			}
		} else {
			pr = &planRun{At: r.At, Tokens: map[string]int64{}, Models: map[string]bool{}}
		}
		prov := r.Provider
		if prov == "" {
			prov = "unknown"
		}
		pr.Tokens[prov] += r.InputTokens + r.OutputTokens + r.CacheWrite
		pr.Models[prov+"/"+r.Model] = true
		pr.Cost += r.CostUSD
		if kind != PlanCouncil {
			out[kind] = append(out[kind], *pr)
		}
	}
	for _, pr := range councils {
		out[PlanCouncil] = append(out[PlanCouncil], *pr)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].At.After(out[k][j].At) })
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// estimate is the median of the most recent runs of a kind that match the
// provider and model, falling back to the provider, then to the kind.
func estimate(runs map[string][]planRun, kind, provider, model string) Estimate {
	pick := func(match func(planRun) bool, tokens func(planRun) int64) (Estimate, bool) {
		var ts, cs []float64
		for _, r := range runs[kind] {
			if !match(r) {
				continue
			}
			ts = append(ts, float64(tokens(r)))
			if r.Cost > 0 {
				cs = append(cs, r.Cost)
			}
			if len(ts) == EstimateRuns {
				break
			}
		}
		if len(ts) == 0 {
			return Estimate{}, false
		}
		return Estimate{Tokens: int64(math.Round(median(ts))), CostUSD: math.Round(median(cs)*100) / 100, Runs: len(ts)}, true
	}
	onProvider := func(r planRun) int64 { return r.Tokens[provider] }
	all := func(r planRun) int64 {
		var n int64
		for _, t := range r.Tokens {
			n += t
		}
		return n
	}
	if model != "" {
		if e, ok := pick(func(r planRun) bool { return r.Models[provider+"/"+model] }, onProvider); ok {
			e.Basis = "model"
			return e
		}
	}
	if e, ok := pick(func(r planRun) bool { return r.Tokens[provider] > 0 }, onProvider); ok {
		e.Basis = "provider"
		return e
	}
	// No run on this provider: what the whole run took on any provider, which
	// for a council overstates one seat's share. An overstatement is the safe
	// side when the question is whether something fits.
	if e, ok := pick(func(planRun) bool { return true }, all); ok {
		e.Basis = "kind"
		return e
	}
	return Estimate{Basis: "none"}
}

// ---- the plan ----

// PlanItem is one thing the user intends to do today.
type PlanItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	// Provider pins the provider; "" lets Lucidbench suggest one.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Project  string `json:"project,omitempty"`
	// Source is "nextup" for an item taken from Next up, else "".
	Source   string `json:"source,omitempty"`
	NextUpID string `json:"nextup_id,omitempty"`
}

// TeamSeat is a filled team role, as the planner reads it.
type TeamSeat struct {
	Role      string
	Provider  string
	Model     string
	BudgetUSD *float64
}

// PlannedItem is an item with its estimate, the provider to use and whether
// it fits before that provider's window resets.
type PlannedItem struct {
	PlanItem
	// Use is the provider the item is planned on; Suggested says Lucidbench
	// chose it, and Reason why.
	Use       string   `json:"use"`
	UseModel  string   `json:"use_model,omitempty"`
	Suggested bool     `json:"suggested"`
	Reason    string   `json:"reason,omitempty"`
	Estimate  Estimate `json:"estimate"`
	// Verdict is fits, after_reset (does not fit before the reset, does
	// after it), unknown (no estimate or no known window) or too_big.
	Verdict  string     `json:"verdict"`
	Fit      string     `json:"fit"`
	Window   string     `json:"window,omitempty"`
	ResetsAt *time.Time `json:"resets_at,omitempty"`
	Warnings []string   `json:"warnings"`
}

// WindowPlan says what fits before one provider's tightest window resets.
type WindowPlan struct {
	Provider  string     `json:"provider"`
	Window    string     `json:"window"`
	ResetsAt  *time.Time `json:"resets_at,omitempty"`
	Remaining int64      `json:"remaining_tokens"`
	Planned   int64      `json:"planned_tokens"`
	Items     int        `json:"items"`
	Fit       int        `json:"fit"`
	Summary   string     `json:"summary"`
}

// PlanInput is everything a plan is computed from.
type PlanInput struct {
	Items     []PlanItem
	Paces     []Pace
	Runs      map[string][]planRun
	Team      func(project string) []TeamSeat
	Available []string // providers signed in on this machine; nil means all of PlanProviders
	Now       time.Time
	Loc       *time.Location
}

// budgetLeft is what is left of a provider's tightest window; known is
// false when the window's size is not known.
type budgetLeft struct {
	known  bool
	left   int64
	window string
	resets *time.Time
	pace   *Pace
}

// tightest is the default account's window with the least left.
func tightest(ps []Pace, provider string) budgetLeft {
	var best budgetLeft
	for i := range ps {
		p := &ps[i]
		if p.Provider != provider || (p.Account != "default" && p.Account != "") {
			continue
		}
		r := p.remaining()
		if r < 0 {
			if !best.known && best.pace == nil {
				best = budgetLeft{window: p.Window, resets: p.ResetsAt, pace: p}
			}
			continue
		}
		if !best.known || r < best.left {
			best = budgetLeft{known: true, left: r, window: p.Window, resets: p.ResetsAt, pace: p}
		}
	}
	return best
}

func seatFor(seats []TeamSeat, role string) *TeamSeat {
	for i := range seats {
		if seats[i].Role == role {
			return &seats[i]
		}
	}
	return nil
}

// Plan computes the plan: items in order, each spending from its
// provider's window, so a later item sees what the earlier ones used.
func Plan(in PlanInput) ([]PlannedItem, []WindowPlan) {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	avail := in.Available
	if avail == nil {
		avail = PlanProviders
	}
	left := map[string]budgetLeft{}
	for _, p := range PlanProviders {
		left[p] = tightest(in.Paces, p)
	}
	windows := map[string]*WindowPlan{}
	out := []PlannedItem{}
	for _, it := range in.Items {
		var seats []TeamSeat
		if in.Team != nil {
			seats = in.Team(it.Project)
		}
		role := planRole[it.Kind]
		seat := seatFor(seats, role)
		pi := PlannedItem{PlanItem: it, Warnings: []string{}}

		// Candidates, in order of preference: the pinned provider alone, or
		// the team's, then the others signed in.
		var cands []string
		if it.Provider != "" {
			cands = []string{it.Provider}
		} else {
			if seat != nil {
				cands = append(cands, seat.Provider)
			}
			for _, p := range avail {
				if !contains(cands, p) {
					cands = append(cands, p)
				}
			}
		}
		type option struct {
			provider, model string
			est             Estimate
			fits            string // yes | no | unknown
		}
		var opts []option
		for _, c := range cands {
			model := it.Model
			if model == "" && seat != nil && seat.Provider == c {
				model = seat.Model
			}
			e := estimate(in.Runs, it.Kind, c, model)
			f := "unknown"
			if b := left[c]; b.known && e.Basis != "none" {
				f = "no"
				if e.Tokens <= b.left {
					f = "yes"
				}
			}
			opts = append(opts, option{c, model, e, f})
		}
		chosen := 0
		if it.Provider == "" && len(opts) > 0 {
			pref := opts[0]
			if seat == nil || seat.Provider != pref.provider {
				// No team preference: a provider with known room comes first.
				for i, o := range opts {
					if o.fits == "yes" {
						opts[0], opts[i] = opts[i], opts[0]
						pref = opts[0]
						break
					}
				}
			}
			switch {
			case pref.fits == "yes" && (seat == nil || seat.Provider != pref.provider):
				pi.Reason = "has room before its reset"
			case pref.fits != "no":
				if seat != nil && seat.Provider == pref.provider {
					pi.Reason = "your team's " + role
				}
			default:
				alt := -1
				for i, o := range opts[1:] {
					if o.fits == "yes" {
						alt = i + 1
						break
					}
				}
				if alt < 0 {
					for i, o := range opts[1:] {
						if o.fits == "unknown" && o.est.Basis != "none" {
							alt = i + 1
							break
						}
					}
				}
				if alt > 0 {
					chosen = alt
					b := left[pref.provider]
					pi.Reason = fmt.Sprintf("%s's %s has about %s tokens left, not enough; %s has room", pref.provider, windowName(b.window), compact(b.left), opts[alt].provider)
					if opts[alt].fits == "unknown" {
						pi.Reason = fmt.Sprintf("%s's %s has about %s tokens left, not enough; %s reports no window, so its room is unknown", pref.provider, windowName(b.window), compact(b.left), opts[alt].provider)
					}
				} else {
					pi.Reason = "no provider has room before its reset"
				}
			}
			pi.Suggested = true
		}
		var o option
		if len(opts) > 0 {
			o = opts[chosen]
		}
		pi.Use, pi.UseModel, pi.Estimate = o.provider, o.model, o.est
		b := left[o.provider]
		pi.Window, pi.ResetsAt = b.window, b.resets
		resetAt := ""
		if b.resets != nil {
			resetAt = " at " + clock(*b.resets, loc)
		}
		switch {
		case o.est.Basis == "none":
			pi.Verdict = "unknown"
			pi.Fit = "No recent runs of this kind to estimate from."
		case !b.known:
			pi.Verdict = "unknown"
			pi.Fit = fmt.Sprintf("%s reports no limit window, so whether it fits is unknown.", o.provider)
			if b.pace != nil {
				pi.Fit = fmt.Sprintf("The size of %s's %s is not known yet, so whether it fits is unknown.", o.provider, windowName(b.window))
			}
		case o.est.Tokens <= b.left:
			pi.Verdict = "fits"
			pi.Fit = fmt.Sprintf("Fits before %s's %s resets%s.", o.provider, windowName(b.window), resetAt)
		case b.pace != nil && b.pace.CapacityTokens > 0 && o.est.Tokens > b.pace.CapacityTokens:
			pi.Verdict = "too_big"
			pi.Fit = fmt.Sprintf("Usually takes more than a whole %s %s; split it up.", o.provider, windowName(b.window))
		default:
			pi.Verdict = "after_reset"
			pi.Fit = fmt.Sprintf("Does not fit before %s's %s resets%s; start it after then.", o.provider, windowName(b.window), resetAt)
		}
		if b.known && o.est.Basis != "none" {
			w := windows[o.provider]
			if w == nil {
				w = &WindowPlan{Provider: o.provider, Window: b.window, ResetsAt: b.resets, Remaining: b.left}
				windows[o.provider] = w
			}
			w.Items++
			if pi.Verdict == "fits" {
				w.Fit++
				w.Planned += o.est.Tokens
				b.left -= o.est.Tokens
				left[o.provider] = b
			}
		}

		// Budgets: a role's budget_usd is what one run should cost at most.
		if o.est.CostUSD > 0 {
			switch it.Kind {
			case PlanCouncil:
				var sum float64
				all := true
				n := 0
				for _, s := range seats {
					if s.Role != "proposer" && s.Role != "critic" {
						continue
					}
					n++
					if s.BudgetUSD == nil {
						all = false
						continue
					}
					sum += *s.BudgetUSD
				}
				if n > 0 && all && o.est.CostUSD > sum {
					pi.Warnings = append(pi.Warnings, fmt.Sprintf("A council has cost about $%.2f lately, over the $%.2f its seats' budgets add up to.", o.est.CostUSD, sum))
				}
			default:
				if seat != nil && seat.BudgetUSD != nil && seat.Provider == o.provider && o.est.CostUSD > *seat.BudgetUSD {
					pi.Warnings = append(pi.Warnings, fmt.Sprintf("A %s on %s has cost about $%.2f lately, over the %s's $%.2f budget.", kindNoun(it.Kind), o.provider, o.est.CostUSD, role, *seat.BudgetUSD))
				}
			}
		}
		out = append(out, pi)
	}

	var wp []WindowPlan
	for _, p := range PlanProviders {
		w := windows[p]
		if w == nil {
			continue
		}
		at := ""
		if w.ResetsAt != nil {
			at = " at " + clock(*w.ResetsAt, loc)
		}
		w.Summary = fmt.Sprintf("Before %s's %s resets%s: %d of %d planned %s fit, using about %s of the %s tokens left.", p, windowName(w.Window), at, w.Fit, w.Items, itemWord(w.Items), compact(w.Planned), compact(w.Remaining))
		wp = append(wp, *w)
	}
	if wp == nil {
		wp = []WindowPlan{}
	}
	return out, wp
}

func kindNoun(k string) string {
	switch k {
	case PlanWork:
		return "Work turn"
	case PlanSection:
		return "section description"
	case PlanRank:
		return "Next up ranking"
	case PlanAssistant:
		return "Assistant turn"
	}
	return k
}

func itemWord(n int) string {
	if n == 1 {
		return "item"
	}
	return "items"
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
