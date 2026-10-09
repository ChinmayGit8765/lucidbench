package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// ownRun is one Lucidbench-run agent invocation, from a council, work or
// Prompt Studio record.
type ownRun struct {
	Source string // council | work | studio | sections | nextup | assistant
	At     time.Time
	agentexec.Usage
}

// timeFields are the record fields tried, in order, for when a run happened.
var timeFields = []string{"started", "started_at", "created", "created_at", "ended", "updated"}

// readOwn reads the usage Lucidbench recorded for its own agent runs under
// the data dir. Missing folders and unreadable or unfamiliar files are
// skipped: a record may carry usage as one object or a list.
func readOwn(dataDir string) []ownRun {
	var out []ownRun
	files := map[string]string{}
	council, _ := filepath.Glob(filepath.Join(dataDir, "council", "*.json"))
	for _, p := range council {
		files[p] = "council"
	}
	work, _ := filepath.Glob(filepath.Join(dataDir, "work", "sessions", "*", "session.json"))
	for _, p := range work {
		files[p] = "work"
	}
	// Prompt Studio's "Improve this prompt" calls, one record each.
	studio, _ := filepath.Glob(filepath.Join(dataDir, "prompts", "runs", "*.json"))
	for _, p := range studio {
		files[p] = "studio"
	}
	// "Describe a section" calls, one record each.
	sections, _ := filepath.Glob(filepath.Join(dataDir, "sections", "runs", "*.json"))
	for _, p := range sections {
		files[p] = "sections"
	}
	// "Ask an agent to rank" calls on Next up, one record each.
	nextup, _ := filepath.Glob(filepath.Join(dataDir, "nextup", "runs", "*.json"))
	for _, p := range nextup {
		files[p] = "nextup"
	}
	// Assistant turns and braindump parses, one record each.
	assistant, _ := filepath.Glob(filepath.Join(dataDir, "assistant", "runs", "*.json"))
	for _, p := range assistant {
		files[p] = "assistant"
	}
	for p, src := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var rec map[string]json.RawMessage
		if json.Unmarshal(b, &rec) != nil {
			continue
		}
		at := time.Time{}
		for _, k := range timeFields {
			var s string
			if json.Unmarshal(rec[k], &s) == nil {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					at = t
					break
				}
			}
		}
		if at.IsZero() {
			if info, err := os.Stat(p); err == nil {
				at = info.ModTime()
			}
		}
		var list []agentexec.Usage
		if json.Unmarshal(rec["usage"], &list) != nil {
			var one agentexec.Usage
			if json.Unmarshal(rec["usage"], &one) != nil {
				continue
			}
			list = []agentexec.Usage{one}
		}
		for _, u := range list {
			out = append(out, ownRun{Source: src, At: at, Usage: u})
		}
	}
	return out
}
