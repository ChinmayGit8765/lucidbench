package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
)

func runAccounts(args []string) int {
	fs := flag.NewFlagSet("accounts", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ps := accounts.All(accounts.FromConfig(cfg))
	if ps == nil {
		ps = []accounts.Profile{}
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(ps)
		return 0
	}
	fmt.Print(accounts.Table(ps))
	return 0
}

// runLogin prepares a profile volume and prints the login command. It never
// runs the login itself.
func runLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile name (required)")
	provider := ""
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		provider, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if provider == "" && fs.NArg() > 0 {
		provider = fs.Arg(0)
	}
	spec, ok := accounts.Providers[provider]
	if !ok || *profile == "" {
		fmt.Fprintln(os.Stderr, "usage: lucid login <claude|codex|grok> --profile <name>")
		return 2
	}
	if !accounts.ProfileNameRE.MatchString(*profile) {
		fmt.Fprintln(os.Stderr, "error: profile name must match "+accounts.ProfileNameRE.String())
		return 2
	}
	vol := accounts.VolumeName(provider, *profile)
	if err := exec.Command("docker", "volume", "inspect", vol).Run(); err != nil {
		out, cerr := exec.Command("docker", "volume", "create",
			"--label", accounts.VolumeLabel+"="+provider+"/"+*profile, vol).CombinedOutput()
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "error: docker volume create failed: %v\n%s", cerr, out)
			return 1
		}
		fmt.Printf("created volume %s\n", vol)
	} else {
		fmt.Printf("volume %s already exists\n", vol)
	}
	fmt.Println("Log in interactively by running:")
	fmt.Printf("  docker run --rm -it -v %s:%s %s %s\n", vol, spec.MountPath, cfg.Agent.Image, spec.LoginCmd)
	return 0
}
