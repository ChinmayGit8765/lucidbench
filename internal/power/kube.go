package power

import (
	"context"
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/jobs"
	"github.com/ChinmayGit8765/lucidbench/internal/k8s"
)

// Kube is the real ClusterAPI: the Lucidbench kubeconfig and client-go.
type Kube struct{}

// Refresh writes the kubeconfig of the existing cluster again.
func (Kube) Refresh() error { return cluster.RefreshKubeconfig() }

// client connects, writing the kubeconfig first when it is missing (a fresh
// data folder, or one whose cluster was created elsewhere).
func (Kube) client() (kubernetes.Interface, error) {
	cs, err := k8s.Clientset()
	if errors.Is(err, k8s.ErrNoCluster) {
		if rerr := cluster.RefreshKubeconfig(); rerr == nil {
			cs, err = k8s.Clientset()
		}
	}
	return cs, err
}

// Ready lists the nodes and requires every one to be Ready.
func (k Kube) Ready(ctx context.Context) error {
	cs, err := k.client()
	if err != nil {
		return err
	}
	return nodesReady(ctx, cs)
}

func nodesReady(ctx context.Context, cs kubernetes.Interface) error {
	l, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(l.Items) == 0 {
		return errors.New("no nodes registered yet")
	}
	for _, n := range l.Items {
		ready := false
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			return fmt.Errorf("node %s is not ready yet", n.Name)
		}
	}
	return nil
}

// Busy counts running or pending pods in the lucidbench namespace and jobs
// there with active pods.
func (k Kube) Busy(ctx context.Context) (int, int, error) {
	cs, err := k.client()
	if err != nil {
		return 0, 0, err
	}
	return busy(ctx, cs)
}

func busy(ctx context.Context, cs kubernetes.Interface) (int, int, error) {
	pl, err := cs.CoreV1().Pods(jobs.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, err
	}
	pods := 0
	for _, p := range pl.Items {
		if p.Status.Phase == corev1.PodRunning || p.Status.Phase == corev1.PodPending {
			pods++
		}
	}
	jl, err := cs.BatchV1().Jobs(jobs.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, err
	}
	active := 0
	for _, j := range jl.Items {
		if j.Status.Active > 0 || !finished(&j) && j.Status.Succeeded == 0 && j.Status.Failed == 0 {
			active++
		}
	}
	return pods, active, nil
}

// finished reports whether a job has a Complete or Failed condition.
func finished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
