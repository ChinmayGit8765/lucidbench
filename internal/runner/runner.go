// Package runner builds and executes `docker run` invocations for provider
// CLIs inside the agent image, with a per-profile lock.
package runner

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Image is the agent image tag.
const Image = "lucidbench/agent:dev"

// HostProfile selects the host's own provider config dir.
const HostProfile = "host"

// providers maps a provider to its config dir name under $HOME.
var providers = map[string]string{
	"claude": ".claude",
	"codex":  ".codex",
	"grok":   ".grok",
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidProvider reports whether p is a supported provider.
func ValidProvider(p string) bool { _, ok := providers[p]; return ok }

// ValidProfile reports whether name is a legal profile name.
func ValidProfile(name string) bool { return nameRe.MatchString(name) }

// Command returns the non-interactive command line for a provider.
func Command(provider, prompt string) ([]string, error) {
	switch provider {
	case "claude":
		return []string{"claude", "-p", prompt}, nil
	case "codex":
		return []string{"codex", "exec", "--skip-git-repo-check", prompt}, nil
	case "grok":
		return []string{"grok", "-p", prompt}, nil
	}
	return nil, fmt.Errorf("unknown provider %q (want claude, codex or grok)", provider)
}

// MountPath is the container path where a provider's config dir lives.
func MountPath(provider string) string { return "/root/" + providers[provider] }

// VolumeName is the named volume for a non-host profile.
func VolumeName(provider, profile string) string {
	return "lucidbench-" + provider + "-" + profile
}

// HostDir returns the host config dir for a provider under home.
func HostDir(home, provider string) string { return filepath.Join(home, providers[provider]) }

var driveRe = regexp.MustCompile(`^([A-Za-z]):[\\/]`)

// DockerPath converts a host path to the form Docker Desktop accepts in -v.
// Windows paths like C:\Users\me become /c/Users/me; others pass through.
func DockerPath(p string) string {
	if m := driveRe.FindStringSubmatch(p); m != nil {
		return "/" + strings.ToLower(m[1]) + strings.ReplaceAll(p[2:], `\`, "/")
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// Args builds the arguments after `docker` for a run. home is the host home
// dir, used only when profile is HostProfile.
func Args(provider, profile, prompt, home string) ([]string, error) {
	cmd, err := Command(provider, prompt)
	if err != nil {
		return nil, err
	}
	var src string
	if profile == HostProfile {
		src = DockerPath(HostDir(home, provider))
	} else {
		if !ValidProfile(profile) {
			return nil, fmt.Errorf("invalid profile name %q", profile)
		}
		src = VolumeName(provider, profile)
	}
	args := []string{"run", "--rm", "-i", "-v", src + ":" + MountPath(provider), Image}
	return append(args, cmd...), nil
}
