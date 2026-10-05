package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
)

const ciUsage = "usage: lucid ci [summary|runners|runs [--repo owner/name]] [--json]"

// runCI implements `lucid ci`, `lucid ci runners` and `lucid ci runs`. It
// reads docker and GitHub directly, so it works without a running daemon.
func runCI(args []string) int {
	sub := "summary"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("ci "+sub, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	repo := ""
	if sub == "runs" {
		fs.StringVar(&repo, "repo", "", "only this repository (owner/name)")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := ci.New(cfg.CI)

	var v any
	switch sub {
	case "summary":
		v = s.Summary(ctx)
	case "runners":
		v = s.Runners(ctx)
	case "runs":
		r, err := s.Runs(ctx, repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", repo, err)
			return 1
		}
		v = r
	default:
		fmt.Fprintln(os.Stderr, ciUsage)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(v)
		return 0
	}
	switch b := v.(type) {
	case ci.Summary:
		printCISummary(b)
	case ci.RunnersResponse:
		printCIRunners(b)
	case ci.RunsResponse:
		printCIRuns(b)
	}
	return 0
}

func printSourceErrors(errs []ci.SourceError) {
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", e.Source, e.Message)
	}
}

func notConfigured(repos []string) bool {
	if len(repos) > 0 {
		return false
	}
	fmt.Println("No repositories configured. Add them to ci.github.repos (see `lucid config path`):")
	fmt.Println()
	fmt.Println("  ci:")
	fmt.Println("    github:")
	fmt.Println(`      repos: ["you/your-repo"]`)
	fmt.Println()
	return true
}

func printCISummary(s ci.Summary) {
	notConfigured(s.Repos)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "repos:\t%d\t(token: %s)\n", len(s.Repos), s.TokenSource)
	fmt.Fprintf(w, "runners:\t%d online\t%d busy, %d offline\n", s.Runners.Online, s.Runners.Busy, s.Runners.Offline)
	fmt.Fprintf(w, "containers:\t%d up\t%d down\n", s.Containers.Up, s.Containers.Down)
	rate := "no completed runs"
	if s.Runs24h.PassRate != nil {
		rate = fmt.Sprintf("%.0f%% pass", *s.Runs24h.PassRate*100)
	}
	fmt.Fprintf(w, "runs (24h):\t%d\t%s, %d in progress\n", s.Runs24h.Total, rate, s.Runs24h.InProgress)
	if len(s.Runs24h.ByConclusion) > 0 {
		keys := make([]string, 0, len(s.Runs24h.ByConclusion))
		for k := range s.Runs24h.ByConclusion {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, s.Runs24h.ByConclusion[k]))
		}
		fmt.Fprintf(w, "\t\t%s\n", strings.Join(parts, ", "))
	}
	failing := "none"
	if len(s.FailingRepos) > 0 {
		failing = strings.Join(s.FailingRepos, ", ")
	}
	fmt.Fprintf(w, "failing repos:\t%s\t\n", failing)
	_ = w.Flush()
	printSourceErrors(s.Errors)
}

func printCIRunners(r ci.RunnersResponse) {
	if !notConfigured(r.Repos) {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "REPO\tRUNNER\tSTATUS\tLABELS\tCONTAINER")
		for _, x := range r.Runners {
			st := x.Status
			if x.Busy {
				st += " (busy)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", x.Repo, x.Name, st, strings.Join(x.Labels, ","), dash(x.Container))
		}
		_ = w.Flush()
		if len(r.Runners) == 0 {
			fmt.Println("(no self-hosted runners registered)")
		}
		fmt.Println()
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CONTAINER\tSTATE\tSTATUS\tSERVICE\tRUNNER\tREPO")
	for _, c := range r.Containers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Name, c.State, c.Status, dash(c.Service), dash(c.RunnerName), dash(c.RepoURL))
	}
	_ = w.Flush()
	if len(r.Containers) == 0 {
		fmt.Println("(no runner containers on this machine)")
	}
	printSourceErrors(r.Errors)
}

func printCIRuns(r ci.RunsResponse) {
	if notConfigured(r.Repos) {
		return
	}
	now := time.Now()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tID\tWORKFLOW\tRESULT\tBRANCH\tEVENT\tCREATED")
	for _, x := range r.Runs {
		res := x.Conclusion
		if x.Status != "completed" || res == "" {
			res = x.Status
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\t%s\n", x.Repo, x.ID, x.Name, res, x.Branch, x.Event, ago(x.CreatedAt, now))
	}
	_ = w.Flush()
	if len(r.Runs) == 0 {
		fmt.Println("(no workflow runs)")
	}
	printSourceErrors(r.Errors)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func ago(iso string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
