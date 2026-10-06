package cloud

import (
	"context"
	"strconv"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

func itoa(n int) string { return strconv.Itoa(n) }

// fetchAz reports who the Azure CLI is signed in as: the subscription it
// prints, never the user or tenant. Nothing else is read.
func fetchAz(ctx context.Context, c *caller, _ []projects.Deploy) Summary {
	var sum Summary
	var acc struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := c.json(ctx, &acc, "account", "show", "--output", "json"); err != nil {
		return failed(sum, err)
	}
	sum.State, sum.Account = StateConnected, firstOf(acc.Name, acc.ID)
	return sum
}

// fetchAws reports the AWS account id from sts get-caller-identity. The
// caller's ARN and user id are not read.
func fetchAws(ctx context.Context, c *caller, _ []projects.Deploy) Summary {
	var sum Summary
	var id struct {
		Account string `json:"Account"`
	}
	if err := c.json(ctx, &id, "sts", "get-caller-identity", "--output", "json"); err != nil {
		return failed(sum, err)
	}
	if id.Account == "" {
		sum.State, sum.Message = StateNotSignedIn, "not signed in"
		return sum
	}
	sum.State, sum.Account = StateConnected, id.Account
	return sum
}
