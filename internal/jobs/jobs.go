// Package jobs submits and inspects batch Jobs on the local Lucidbench cluster.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
)

// Namespace holds all Lucidbench jobs.
const Namespace = "lucidbench"

// ErrNoCluster means the local cluster has not been created (run `lucid cluster up`).
var ErrNoCluster = errors.New("no local cluster: run `lucid cluster up`")

// Info summarises a Job.
type Info struct {
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// Runner talks to the cluster through a client-go clientset.
type Runner struct {
	cs kubernetes.Interface
}

// NewRunner wraps an existing clientset (used by tests).
func NewRunner(cs kubernetes.Interface) *Runner { return &Runner{cs: cs} }

// Connect builds a Runner from the Lucidbench kubeconfig.
func Connect() (*Runner, error) {
	if cluster.InContainer() {
		return connectInternal()
	}
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
	cfg, err := clientcmd.BuildConfigFromFlags("", kc)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	cfg.Timeout = 15 * time.Second
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Runner{cs: cs}, nil
}

// connectInternal builds a Runner from an in-memory kubeconfig that points at
// the kind node's address on the shared docker network.
func connectInternal() (*Runner, error) {
	raw, err := cluster.InternalKubeconfig()
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, ErrNoCluster
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	cfg.Timeout = 15 * time.Second
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Runner{cs: cs}, nil
}

// BuildJob returns the Job spec for a command run in an image.
func BuildJob(name, image string, command []string) *batchv1.Job {
	ttl := int32(3600)
	backoff := int32(0)
	labels := map[string]string{"app.kubernetes.io/managed-by": "lucidbench"}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "main",
						Image:   image,
						Command: command,
					}},
				},
			},
		},
	}
}

func (r *Runner) ensureNamespace(ctx context.Context) error {
	_, err := r.cs.CoreV1().Namespaces().Get(ctx, Namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = r.cs.CoreV1().Namespaces().Create(ctx,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace}}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// Submit creates a Job, creating the namespace first if needed.
func (r *Runner) Submit(ctx context.Context, name, image string, command []string) (*batchv1.Job, error) {
	if err := r.ensureNamespace(ctx); err != nil {
		return nil, fmt.Errorf("ensure namespace: %w", err)
	}
	return r.cs.BatchV1().Jobs(Namespace).Create(ctx, BuildJob(name, image, command), metav1.CreateOptions{})
}

// HelloImage is the image the hello job runs.
const HelloImage = "busybox:1.36"

// HelloCommand is what the hello job runs.
var HelloCommand = []string{"sh", "-c", "echo hello from lucidbench && date"}

// SubmitHello submits a uniquely named hello job.
func (r *Runner) SubmitHello(ctx context.Context) (*batchv1.Job, error) {
	name := fmt.Sprintf("hello-%d", time.Now().UnixMilli())
	return r.Submit(ctx, name, HelloImage, HelloCommand)
}

func status(j *batchv1.Job) string {
	for _, c := range j.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return "Completed"
		case batchv1.JobFailed:
			return "Failed"
		}
	}
	if j.Status.Active > 0 {
		return "Running"
	}
	return "Pending"
}

// List returns all Lucidbench jobs, newest first.
func (r *Runner) List(ctx context.Context) ([]Info, error) {
	l, err := r.cs.BatchV1().Jobs(Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return []Info{}, nil
		}
		return nil, err
	}
	out := make([]Info, 0, len(l.Items))
	for i := range l.Items {
		j := &l.Items[i]
		img := ""
		if cs := j.Spec.Template.Spec.Containers; len(cs) > 0 {
			img = cs[0].Image
		}
		out = append(out, Info{Name: j.Name, Image: img, Status: status(j), CreatedAt: j.CreationTimestamp.Time})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.After(out[b].CreatedAt) })
	return out, nil
}

// Logs streams the logs of the Job's pod into w.
func (r *Runner) Logs(ctx context.Context, name string, w io.Writer) error {
	pods, err := r.cs.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + name})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pod found for job %q", name)
	}
	rc, err := r.cs.CoreV1().Pods(Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(w, rc)
	return err
}
