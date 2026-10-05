// Package k8s reads the local Lucidbench cluster for the Kubernetes page:
// namespaces, nodes, pods, jobs, recent events and pod logs. The only change
// it can make is deleting a job that has finished.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
)

// ErrNoCluster means the local cluster has not been created.
var ErrNoCluster = errors.New("no local cluster: run `lucid cluster up`")

// RESTConfig loads the Lucidbench kubeconfig. Inside a container it is
// generated in memory and points at the kind node on the docker network.
func RESTConfig() (*rest.Config, error) {
	var cfg *rest.Config
	if cluster.InContainer() {
		raw, err := cluster.InternalKubeconfig()
		if err != nil {
			return nil, err
		}
		if raw == "" {
			return nil, ErrNoCluster
		}
		cfg, err = clientcmd.RESTConfigFromKubeConfig([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
	} else {
		kc, err := cluster.KubeconfigPath()
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(kc); err != nil {
			if os.IsNotExist(err) {
				return nil, ErrNoCluster
			}
			return nil, err
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kc)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
	}
	cfg.Timeout = 15 * time.Second
	return cfg, nil
}

// Clientset connects to the local cluster.
func Clientset() (kubernetes.Interface, error) {
	cfg, err := RESTConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// Client reads the cluster.
type Client struct {
	CS kubernetes.Interface
}

// Node is one cluster node.
type Node struct {
	Name       string    `json:"name"`
	Ready      bool      `json:"ready"`
	Roles      []string  `json:"roles"`
	Version    string    `json:"version"`
	OS         string    `json:"os"`
	Runtime    string    `json:"runtime"`
	CPU        string    `json:"cpu"`
	Memory     string    `json:"memory"`
	Pods       int       `json:"pods"`
	CreatedAt  time.Time `json:"created_at"`
	Conditions []string  `json:"pressure"`
}

// Pod is one pod.
type Pod struct {
	Namespace  string    `json:"namespace"`
	Name       string    `json:"name"`
	Phase      string    `json:"phase"`
	Reason     string    `json:"reason,omitempty"`
	Ready      string    `json:"ready"`
	Restarts   int32     `json:"restarts"`
	Node       string    `json:"node"`
	Containers []string  `json:"containers"`
	CreatedAt  time.Time `json:"created_at"`
	Owner      string    `json:"owner,omitempty"`
}

// Job is one batch job.
type Job struct {
	Namespace   string     `json:"namespace"`
	Name        string     `json:"name"`
	Status      string     `json:"status"` // Running | Completed | Failed | Pending
	Finished    bool       `json:"finished"`
	Succeeded   int32      `json:"succeeded"`
	Failed      int32      `json:"failed"`
	Image       string     `json:"image"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Event is one cluster event.
type Event struct {
	Namespace string    `json:"namespace"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Object    string    `json:"object"`
	Message   string    `json:"message"`
	Count     int32     `json:"count"`
	At        time.Time `json:"at"`
}

// Namespaces lists namespace names, alphabetically.
func (c *Client) Namespaces(ctx context.Context) ([]string, error) {
	l, err := c.CS.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(l.Items))
	for _, n := range l.Items {
		out = append(out, n.Name)
	}
	sort.Strings(out)
	return out, nil
}

// Nodes lists the nodes with their pod counts.
func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	l, err := c.CS.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	pods, _ := c.CS.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	out := make([]Node, 0, len(l.Items))
	for _, n := range l.Items {
		nd := Node{
			Name:       n.Name,
			Version:    n.Status.NodeInfo.KubeletVersion,
			OS:         n.Status.NodeInfo.OSImage,
			Runtime:    n.Status.NodeInfo.ContainerRuntimeVersion,
			CPU:        n.Status.Capacity.Cpu().String(),
			Memory:     n.Status.Capacity.Memory().String(),
			CreatedAt:  n.CreationTimestamp.Time,
			Roles:      []string{},
			Conditions: []string{},
		}
		for k := range n.Labels {
			if r, ok := strings.CutPrefix(k, "node-role.kubernetes.io/"); ok && r != "" {
				nd.Roles = append(nd.Roles, r)
			}
		}
		sort.Strings(nd.Roles)
		for _, cond := range n.Status.Conditions {
			switch {
			case cond.Type == corev1.NodeReady:
				nd.Ready = cond.Status == corev1.ConditionTrue
			case cond.Status == corev1.ConditionTrue:
				nd.Conditions = append(nd.Conditions, string(cond.Type))
			}
		}
		if pods != nil {
			for _, p := range pods.Items {
				if p.Spec.NodeName == n.Name && p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
					nd.Pods++
				}
			}
		}
		out = append(out, nd)
	}
	return out, nil
}

// Pods lists pods in ns ("" for all namespaces), newest first.
func (c *Client) Pods(ctx context.Context, ns string) ([]Pod, error) {
	l, err := c.CS.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Pod, 0, len(l.Items))
	for _, p := range l.Items {
		pd := Pod{
			Namespace:  p.Namespace,
			Name:       p.Name,
			Phase:      string(p.Status.Phase),
			Reason:     p.Status.Reason,
			Node:       p.Spec.NodeName,
			CreatedAt:  p.CreationTimestamp.Time,
			Containers: []string{},
		}
		for _, ct := range p.Spec.Containers {
			pd.Containers = append(pd.Containers, ct.Name)
		}
		ready := 0
		for _, st := range p.Status.ContainerStatuses {
			pd.Restarts += st.RestartCount
			if st.Ready {
				ready++
			}
			// A waiting reason (CrashLoopBackOff, ImagePullBackOff) says more
			// than the phase.
			if st.State.Waiting != nil && st.State.Waiting.Reason != "" {
				pd.Reason = st.State.Waiting.Reason
			}
		}
		pd.Ready = fmt.Sprintf("%d/%d", ready, len(p.Spec.Containers))
		if len(p.OwnerReferences) > 0 {
			pd.Owner = p.OwnerReferences[0].Kind + "/" + p.OwnerReferences[0].Name
		}
		out = append(out, pd)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].CreatedAt.After(out[b].CreatedAt) })
	return out, nil
}

// JobStatus classifies a job.
func JobStatus(j *batchv1.Job) (status string, finished bool) {
	for _, c := range j.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return "Completed", true
		case batchv1.JobFailed:
			return "Failed", true
		}
	}
	if j.Status.Active > 0 {
		return "Running", false
	}
	return "Pending", false
}

// Jobs lists jobs in ns ("" for all namespaces), newest first.
func (c *Client) Jobs(ctx context.Context, ns string) ([]Job, error) {
	l, err := c.CS.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(l.Items))
	for i := range l.Items {
		j := &l.Items[i]
		st, fin := JobStatus(j)
		jb := Job{
			Namespace: j.Namespace, Name: j.Name, Status: st, Finished: fin,
			Succeeded: j.Status.Succeeded, Failed: j.Status.Failed, CreatedAt: j.CreationTimestamp.Time,
		}
		if cs := j.Spec.Template.Spec.Containers; len(cs) > 0 {
			jb.Image = cs[0].Image
		}
		if j.Status.CompletionTime != nil {
			t := j.Status.CompletionTime.Time
			jb.CompletedAt = &t
		}
		out = append(out, jb)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].CreatedAt.After(out[b].CreatedAt) })
	return out, nil
}

// Events returns the newest limit events in ns ("" for all), newest first.
func (c *Client) Events(ctx context.Context, ns string, limit int) ([]Event, error) {
	l, err := c.CS.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(l.Items))
	for _, e := range l.Items {
		at := e.LastTimestamp.Time
		if at.IsZero() {
			at = e.EventTime.Time
		}
		if at.IsZero() {
			at = e.CreationTimestamp.Time
		}
		out = append(out, Event{
			Namespace: e.Namespace,
			Type:      e.Type,
			Reason:    e.Reason,
			Object:    e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Message:   e.Message,
			Count:     e.Count,
			At:        at,
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].At.After(out[b].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// LogOptions builds pod log options for a tail, optionally following.
func LogOptions(container string, tail int64, follow bool) *corev1.PodLogOptions {
	return &corev1.PodLogOptions{Container: container, TailLines: &tail, Follow: follow, Timestamps: true}
}

// Logs opens a pod's log stream; the caller closes it.
func (c *Client) Logs(ctx context.Context, ns, pod, container string, tail int64, follow bool) (io.ReadCloser, error) {
	return c.CS.CoreV1().Pods(ns).GetLogs(pod, LogOptions(container, tail, follow)).Stream(ctx)
}

// Errors returned by DeleteJob.
var (
	ErrNotFinished = errors.New("only a finished job (completed or failed) can be deleted")
	ErrNotFound    = errors.New("no such job")
)

// DeleteJob deletes a finished job and its pods.
func (c *Client) DeleteJob(ctx context.Context, ns, name string) error {
	j, err := c.CS.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, fin := JobStatus(j); !fin {
		return ErrNotFinished
	}
	bg := metav1.DeletePropagationBackground
	return c.CS.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &bg})
}
