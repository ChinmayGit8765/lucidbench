package projects

import (
	"slices"
	"strings"
	"testing"
)

func TestDeployEntries(t *testing.T) {
	ps, errs := Parse([]byte(`version: 1
projects:
  - id: shop
    name: Shop
    category: product
    status: active
    visibility: public
    deploy:
      - { provider: gcloud, service: api, region: us-central1, project: shop-prod }
      - { provider: vercel, service: web }
      - { provider: wrangler, service: edge }
`))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	want := []Deploy{{"gcloud", "api", "us-central1", "shop-prod"}, {"vercel", "web", "", ""}, {"wrangler", "edge", "", ""}}
	if !slices.Equal(ps[0].Deploy, want) {
		t.Errorf("deploy = %+v", ps[0].Deploy)
	}
}

func TestDeployValidation(t *testing.T) {
	ps, errs := Parse([]byte(`version: 1
projects:
  - id: shop
    name: Shop
    category: product
    status: active
    visibility: public
    deploy:
      - { provider: heroku, service: web }
      - { provider: aws, service: web }
      - { provider: vercel }
      - { provider: vercel, service: web, region: iad1 }
      - { provider: wrangler, service: edge, project: p }
      - { provider: vercel, service: ok }
`))
	got := strings.Join(errs, "\n")
	for _, want := range []string{
		`deploy[0].provider: unknown value "heroku"`,
		`deploy[1].provider: unknown value "aws"`,
		"deploy[2].service: required",
		"deploy[3].region: only used with provider gcloud",
		"deploy[4].project: only used with provider gcloud",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing error %q in:\n%s", want, got)
		}
	}
	if len(ps[0].Deploy) != 1 || ps[0].Deploy[0].Service != "ok" {
		t.Errorf("only the valid entry should be kept: %+v", ps[0].Deploy)
	}
}
