// Package jobs submits and inspects batch Jobs on the local Lucidbench cluster.
package jobs

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/ChinmayGit8765/lucidbench/internal/k8s"
)

// Namespace holds all Lucidbench jobs.
const Namespace = "lucidbench"

// ErrNoCluster means the local cluster has not been created (run `lucid cluster up`).
var ErrNoCluster = k8s.ErrNoCluster

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
	cs, err := k8s.Clientset()
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
	s, _ := k8s.JobStatus(j)
	return s
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
