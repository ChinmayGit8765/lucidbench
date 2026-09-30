package jobs

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildJob(t *testing.T) {
	j := BuildJob("x", "busybox", []string{"echo", "hi"})
	if j.Namespace != Namespace || j.Name != "x" {
		t.Fatalf("bad meta: %+v", j.ObjectMeta)
	}
	if j.Spec.BackoffLimit == nil || *j.Spec.BackoffLimit != 0 {
		t.Error("backoffLimit must be 0")
	}
	if j.Spec.TTLSecondsAfterFinished == nil || *j.Spec.TTLSecondsAfterFinished <= 0 {
		t.Error("ttlSecondsAfterFinished must be set")
	}
	p := j.Spec.Template.Spec
	if p.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restartPolicy = %s", p.RestartPolicy)
	}
	if len(p.Containers) != 1 || p.Containers[0].Image != "busybox" || len(p.Containers[0].Command) != 2 {
		t.Errorf("bad container: %+v", p.Containers)
	}
}

func TestSubmitCreatesNamespaceAndJob(t *testing.T) {
	cs := fake.NewSimpleClientset()
	r := NewRunner(cs)
	ctx := context.Background()
	if _, err := r.Submit(ctx, "a", "busybox", []string{"true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Submit(ctx, "b", "busybox", []string{"true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Namespaces().Get(ctx, Namespace, metav1.GetOptions{}); err != nil {
		t.Fatalf("namespace not created: %v", err)
	}
	list, err := r.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestListStatus(t *testing.T) {
	now := time.Now()
	done := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: Namespace, CreationTimestamp: metav1.NewTime(now)},
		Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}},
	}
	failed := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: Namespace, CreationTimestamp: metav1.NewTime(now.Add(-time.Minute))},
		Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}},
	}
	running := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: Namespace, CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Minute))},
		Status:     batchv1.JobStatus{Active: 1},
	}
	r := NewRunner(fake.NewSimpleClientset(done, failed, running))
	list, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Completed", "Failed", "Running"}
	for i, w := range want {
		if list[i].Status != w {
			t.Errorf("list[%d] = %s %s, want %s", i, list[i].Name, list[i].Status, w)
		}
	}
}
