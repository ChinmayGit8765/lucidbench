// Package cluster manages the local kind cluster that runs Lucidbench jobs.
// It uses kind as a library, so no kind binary is required.
package cluster

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"

	kindcfg "github.com/ChinmayGit8765/lucidbench/deploy/kind"
)

// Name is the kind cluster name.
const Name = "lucidbench"

// KubeconfigPath is where Lucidbench keeps its kubeconfig, kept separate from
// the user's default kubeconfig.
func KubeconfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, "lucidbench", "kubeconfig"), nil
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

// Status reports whether the cluster exists.
func Status() (running bool, kubeconfig string, err error) {
	kc, err := KubeconfigPath()
	if err != nil {
		return false, "", err
	}
	ok, err := exists(provider())
	return ok, kc, err
}
