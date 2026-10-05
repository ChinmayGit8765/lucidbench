package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/runner"
)

// runCmd implements `lucid run [--profile p] <provider> "<prompt>"`.
func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	profile := fs.String("profile", runner.HostProfile, "profile: host or a named volume")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, `usage: lucid run [--profile p] <claude|codex|grok> "<prompt>"`)
		return 2
	}
	provider, prompt := fs.Arg(0), fs.Arg(1)
	if !runner.ValidProvider(provider) {
		fmt.Fprintf(os.Stderr, "error: unknown provider %q (want claude, codex or grok)\n", provider)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	lockPath, err := runner.LockPath(provider, *profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	dataDir, err := config.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	// One-shot headless prompt: give the container an empty stdin so CLIs
	// that append piped stdin to the prompt (codex) do not wait on the terminal.
	code, refreshed, err := runner.Run(runner.Options{
		Provider: provider, Profile: *profile, Prompt: prompt,
		Home: home, DataDir: dataDir, LockPath: lockPath,
	}, func(dockerArgs []string) int { return runDocker(dockerArgs, false) })
	for _, n := range refreshed {
		fmt.Fprintf(os.Stderr, "note: %s refreshed its login; copied %s back to the host\n", provider, n)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// imageCmd implements `lucid image build`.
func imageCmd(args []string) int {
	if len(args) != 1 || args[0] != "build" {
		fmt.Fprintln(os.Stderr, "usage: lucid image build")
		return 2
	}
	return runDocker([]string{"build", "-t", runner.Image, "images/agent"}, true)
}

func runDocker(args []string, inheritStdin bool) int {
	c := exec.Command("docker", args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if inheritStdin {
		c.Stdin = os.Stdin
	}
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
