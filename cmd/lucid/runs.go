package main

import (
	"fmt"
	"os"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/runner"
)

// runRuns implements `lucid runs sweep`: delete run folders left behind by a
// crash, and say what was kept and why.
func runRuns(args []string) int {
	if len(args) != 1 || args[0] != "sweep" {
		fmt.Fprintln(os.Stderr, "usage: lucid runs sweep")
		return 2
	}
	data, err := config.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	res, err := runner.Sweep(data, runner.SweepGrace)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	for _, l := range res.Lines() {
		fmt.Println(l)
	}
	if len(res.Failed) > 0 {
		return 1
	}
	return 0
}
