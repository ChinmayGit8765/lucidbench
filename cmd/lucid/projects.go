package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

const projectsUsage = "usage: lucid projects [--json] | projects show <id> | projects init"

// runProjects implements `lucid projects`, `projects show <id>` and
// `projects init`.
func runProjects(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "show":
			if len(args) != 2 {
				fmt.Fprintln(os.Stderr, projectsUsage)
				return 2
			}
			return projectsShow(args[1])
		case "init":
			return projectsInit()
		}
	}
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, projectsUsage)
		return 2
	}
	l, err := projects.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(l)
		return 0
	}
	if !l.Configured {
		fmt.Printf("No projects file at %s.\nRun `lucid projects init` to write an example.\n", l.PathHint)
		return 0
	}
	fmt.Print(projects.Table(l.Projects))
	return reportErrors(l)
}

func reportErrors(l *projects.List) int {
	if len(l.Errors) == 0 {
		return 0
	}
	fmt.Fprintf(os.Stderr, "\n%d problem(s) in %s:\n", len(l.Errors), l.PathHint)
	for _, e := range l.Errors {
		fmt.Fprintln(os.Stderr, "  "+e)
	}
	return 1
}

func projectsShow(id string) int {
	l, err := projects.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	p := l.Find(id)
	if p == nil {
		fmt.Fprintf(os.Stderr, "error: no project %q in %s\n", id, l.PathHint)
		return 1
	}
	fmt.Print(projects.Detail(*p))
	return 0
}

func projectsInit() int {
	path, err := projects.Path()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "error: %s already exists; edit it instead\n", path)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(projects.Template), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("wrote", path)
	return 0
}
