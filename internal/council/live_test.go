package council

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// TestLiveCritique has each installed provider CLI critique one small draft
// with the real critique prompt, and logs the verdict, time and usage. It
// costs a few cents, so it only runs with LUCID_COUNCIL_LIVE=1.
func TestLiveCritique(t *testing.T) {
	if os.Getenv("LUCID_COUNCIL_LIVE") != "1" {
		t.Skip("set LUCID_COUNCIL_LIVE=1 to run the provider CLIs")
	}
	prompt := critiquePrompt(dump, "", brief("Clean up stale run folders"), "", false)
	for _, p := range []string{"claude", "codex", "grok"} {
		t.Run(p, func(t *testing.T) {
			if _, err := exec.LookPath(p); err != nil {
				t.Skipf("%s is not installed", p)
			}
			start := time.Now()
			res, err := agentexec.Run(context.Background(), agentexec.Request{
				Provider: p, SystemPrompt: systemPrompt(CritiquePrompt), Prompt: prompt, Tools: agentexec.ToolsNone, Timeout: 8 * time.Minute,
			})
			if errors.Is(err, agentexec.ErrNotSignedIn) {
				t.Skipf("%s is not signed in", p)
			}
			if err != nil {
				t.Fatalf("after %s: %v", time.Since(start).Round(time.Second), err)
			}
			verdict, points, perr := parseCritique(res.Text)
			t.Logf("%s took %s: verdict %q, %d points, usage %+v", p, time.Since(start).Round(time.Second), verdict, len(points), res.Usage)
			if perr != nil {
				t.Errorf("critique not readable: %v\n%s", perr, res.Text)
			}
		})
	}
}
