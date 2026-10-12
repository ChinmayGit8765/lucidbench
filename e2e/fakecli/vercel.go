package main

// The fake vercel CLI. It is signed out until a spec writes
// $LUCID_E2E_STATE/vercel.json, a map from the subcommand to the JSON the real
// CLI would print: "whoami", "ls" (deployments) and "project" (project ls).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func vercel(args []string) int {
	b, err := os.ReadFile(filepath.Join(stateDir(), "vercel.json"))
	if err != nil || len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No existing credentials found. Please run `vercel login`.")
		return 1
	}
	var answers map[string]json.RawMessage
	if json.Unmarshal(b, &answers) != nil {
		return 1
	}
	if a, ok := answers[args[0]]; ok {
		fmt.Println(string(a))
		return 0
	}
	fmt.Fprintln(os.Stderr, "fake vercel: not supported in tests:", args)
	return 1
}
