package accounts

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// VolumeLabel is the Docker label that marks a profile volume; its value is
// "<provider>/<name>".
const VolumeLabel = "lucidbench.profile"

// Providers that can be logged in inside the agent image, with the config
// directory the volume mounts at and the interactive login command.
var Providers = map[string]struct{ MountPath, LoginCmd string }{
	"claude": {"/root/.claude", "claude"},
	"codex":  {"/root/.codex", "codex login"},
	"grok":   {"/root/.grok", "grok login"},
}

// VolumeName is the Docker volume name for a profile.
func VolumeName(provider, name string) string { return "lucidbench-" + provider + "-" + name }

// ProfileNameRE limits profile names to something safe in a volume name.
var ProfileNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,40}$`)

// VolumeListArgs are the docker arguments used to list profile volumes.
var VolumeListArgs = []string{"volume", "ls", "--filter", "label=" + VolumeLabel,
	"--format", "{{.Name}}\t{{.Labels}}"}

// ParseVolumes parses `docker volume ls --format "{{.Name}}\t{{.Labels}}"`
// output into volume profiles. Volumes without a valid label are skipped.
// Volume status is unknown: verifying it would mean mounting the volume.
func ParseVolumes(out string) []Profile {
	var ps []Profile
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		vol, labels, ok := strings.Cut(line, "\t")
		if !ok || vol == "" {
			continue
		}
		val := ""
		for _, kv := range strings.Split(labels, ",") {
			if k, v, ok := strings.Cut(kv, "="); ok && k == VolumeLabel {
				val = v
			}
		}
		provider, name, ok := strings.Cut(val, "/")
		if !ok || provider == "" || name == "" {
			continue
		}
		ps = append(ps, Profile{Provider: provider, Name: name, Location: LocVolume,
			ConfigDir: vol, Status: StatusUnknown, Detail: "profile volume; login state not inspected"})
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Provider != ps[j].Provider {
			return ps[i].Provider < ps[j].Provider
		}
		return ps[i].Name < ps[j].Name
	})
	return ps
}

// ListVolumes lists profile volumes through the docker CLI. If docker is
// missing or fails, it returns nil: volumes are simply skipped.
func ListVolumes() []Profile {
	path, err := exec.LookPath("docker")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, VolumeListArgs...).Output()
	if err != nil {
		return nil
	}
	return ParseVolumes(string(out))
}

// All returns host, env and volume profiles.
func All(r Roots) []Profile {
	out := Detect(r)
	for _, v := range ListVolumes() {
		if r.On(v.Provider) {
			out = append(out, v)
		}
	}
	return out
}
