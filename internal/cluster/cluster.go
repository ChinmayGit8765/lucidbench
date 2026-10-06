// Package cluster manages the local kind cluster that runs Lucidbench jobs.
// It uses kind as a library, so no kind binary is required.
package cluster

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"

	kindcfg "github.com/ChinmayGit8765/lucidbench/deploy/kind"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Name is the kind cluster name. The daemon and CLI set it from config
// (cluster.name) at startup.
var Name = "lucidbench"

// KubeconfigPath is where Lucidbench keeps its kubeconfig, kept separate from
// the user's default kubeconfig.
func KubeconfigPath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kubeconfig"), nil
}

func provider() *cluster.Provider {
	return cluster.NewProvider(cluster.ProviderWithDocker(), cluster.ProviderWithLogger(cmd.NewLogger()))
}

func exists(p *cluster.Provider) (bool, error) {
	names, err := p.List()
	if err != nil {
		return false, fmt.Errorf("list kind clusters (is Docker running?): %w", err)
	}
	for _, n := range names {
		if n == Name {
			return true, nil
		}
	}
	return false, nil
}

// Up creates the cluster if it does not exist and writes its kubeconfig.
// It is idempotent.
func Up() error {
	kc, err := KubeconfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(kc), 0o700); err != nil {
		return err
	}
	p := provider()
	ok, err := exists(p)
	if err != nil {
		return err
	}
	if ok {
		// Refresh the kubeconfig in case it was deleted.
		return p.ExportKubeConfig(Name, kc, false)
	}
	return p.Create(Name,
		cluster.CreateWithRawConfig(kindcfg.Config),
		cluster.CreateWithKubeconfigPath(kc),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	)
}

// RefreshKubeconfig writes the kubeconfig of an existing cluster again, for
// example after its node was restarted. Unlike Up it never creates a
// cluster. Inside a container the kubeconfig is built on demand, so there is
// nothing to write.
func RefreshKubeconfig() error {
	if InContainer() {
		return nil
	}
	kc, err := KubeconfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(kc), 0o700); err != nil {
		return err
	}
	p := provider()
	ok, err := exists(p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no kind cluster named %q", Name)
	}
	return p.ExportKubeConfig(Name, kc, false)
}

// Down deletes the cluster and its kubeconfig. It is idempotent.
func Down() error {
	kc, err := KubeconfigPath()
	if err != nil {
		return err
	}
	if err := provider().Delete(Name, kc); err != nil {
		return err
	}
	if err := os.Remove(kc); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// InContainer reports whether lucidd runs inside docker compose
// (LUCID_IN_CONTAINER=1), where the host kubeconfig is unreachable.
func InContainer() bool { return os.Getenv("LUCID_IN_CONTAINER") == "1" }

// InternalKubeconfig returns a kubeconfig whose server address is the kind
// node's name on the docker network, reachable from another container on that
// network. It returns "" when the cluster does not exist.
func InternalKubeconfig() (string, error) {
	p := provider()
	ok, err := exists(p)
	if err != nil || !ok {
		return "", err
	}
	if err := joinKindNetwork(); err != nil {
		return "", err
	}
	return p.KubeConfig(Name, true)
}

// KeepJoined joins the kind network as soon as the cluster exists and
// re-checks every interval, so the join (which briefly drops open connections)
// happens in the background rather than in the middle of an API request.
func KeepJoined(every time.Duration) {
	for {
		if ok, err := exists(provider()); err == nil && ok {
			if err := joinKindNetwork(); err != nil {
				log.Print(err)
			}
		}
		time.Sleep(every)
	}
}

// joinKindNetwork attaches this container to the "kind" docker network so the
// internal kubeconfig's server address resolves. Compose cannot declare the
// network up front, because it only exists after `lucid cluster up`; joining
// lazily lets `docker compose up` work on a machine with no cluster yet.
func joinKindNetwork() error {
	self, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("container hostname: %w", err)
	}
	out, err := exec.Command("docker", "network", "connect", "kind", self).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "already exists") {
		return fmt.Errorf("join kind network: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Info is the body of GET /api/cluster.
type Info struct {
	Name              string `json:"name"`
	Running           bool   `json:"running"`
	KubeconfigPresent bool   `json:"kubeconfig_present"`
}

// Describe reports cluster state for the API. Inside a container the
// kubeconfig is generated on demand, so it counts as present when the cluster
// runs.
func Describe() (Info, error) {
	running, kc, err := Status()
	if err != nil {
		return Info{Name: Name}, err
	}
	present := running && InContainer()
	if !present {
		_, statErr := os.Stat(kc)
		present = statErr == nil
	}
	return Info{Name: Name, Running: running, KubeconfigPresent: present}, nil
}

// Status reports whether the cluster exists.
func Status() (running bool, kubeconfig string, err error) {
	kc, err := KubeconfigPath()
	if err != nil {
		return false, "", err
	}
	ok, err := exists(provider())
	return ok, kc, err
}
