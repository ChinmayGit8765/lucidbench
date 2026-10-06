package config

import (
	"strings"
	"testing"
)

func TestIntegrationsDefaults(t *testing.T) {
	c := Default()
	i := c.Integrations
	if i.Linear.Token != "env:LINEAR_API_KEY" || i.Linear.APIURL != "https://api.linear.app/graphql" ||
		i.Trello.Key != "env:TRELLO_API_KEY" || i.Trello.Token != "env:TRELLO_TOKEN" || i.Trello.APIURL != "https://api.trello.com/1" {
		t.Fatalf("integrations defaults %+v", i)
	}
}

func TestIntegrationsFileThenEnv(t *testing.T) {
	p := write(t, "integrations:\n  linear:\n    token: env:MY_LINEAR\n  trello:\n    key: env:MY_KEY\n    token: env:MY_TOKEN\n    api_url: http://localhost:9\n")
	c, warns, err := LoadFrom(p, env(map[string]string{"LUCID_INTEGRATIONS_TRELLO_TOKEN": "env:OTHER", "LUCID_INTEGRATIONS_LINEAR_API_URL": "http://localhost:8"}))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	i := c.Integrations
	if i.Linear.Token != "env:MY_LINEAR" || i.Trello.Key != "env:MY_KEY" || i.Trello.Token != "env:OTHER" ||
		i.Trello.APIURL != "http://localhost:9" || i.Linear.APIURL != "http://localhost:8" {
		t.Fatalf("integrations %+v", i)
	}
	if c.Source("integrations.linear.token") != "file" || c.Source("integrations.trello.token") != "env:LUCID_INTEGRATIONS_TRELLO_TOKEN" {
		t.Fatalf("sources %q %q", c.Source("integrations.linear.token"), c.Source("integrations.trello.token"))
	}
}

func TestIntegrationsRejectLiteralWithoutEcho(t *testing.T) {
	const lit = "lin_api_literal_value_12345"
	p := write(t, "integrations:\n  linear:\n    token: "+lit+"\n")
	_, _, err := LoadFrom(p, env(nil))
	if err == nil || !strings.Contains(err.Error(), "integrations.linear.token") || strings.Contains(err.Error(), lit) {
		t.Fatalf("err = %v", err)
	}
	_, _, err = LoadFrom(write(t, ""), env(map[string]string{"LUCID_INTEGRATIONS_TRELLO_KEY": lit}))
	if err == nil || !strings.Contains(err.Error(), "integrations.trello.key") || strings.Contains(err.Error(), lit) {
		t.Fatalf("env err = %v", err)
	}
}

func TestIntegrationsUnknownKeyWarns(t *testing.T) {
	_, warns, err := LoadFrom(write(t, "integrations:\n  linear:\n    tokn: env:X\n  jira: {}\n"), env(nil))
	if err != nil || len(warns) != 2 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
}
