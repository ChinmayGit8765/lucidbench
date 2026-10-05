package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

var t0 = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

func job(name string, cond batchv1.JobConditionType, active int32) *batchv1.Job {
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "lucidbench", CreationTimestamp: metav1.NewTime(t0)},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main", Image: "busybox:1.36"}},
		}}},
		Status: batchv1.JobStatus{Active: active},
	}
	if cond != "" {
		j.Status.Conditions = []batchv1.JobCondition{{Type: cond, Status: corev1.ConditionTrue}}
	}
	return j
}

func objects() []runtime.Object {
	return []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "lucidbench"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "lucidbench-control-plane", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}},
			Status: corev1.NodeStatus{
				Capacity:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi")},
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}, {Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse}},
				NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.37.0"},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "hello-1-abc", Namespace: "lucidbench", CreationTimestamp: metav1.NewTime(t0),
				OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "hello-1"}}},
			Spec: corev1.PodSpec{NodeName: "lucidbench-control-plane", Containers: []corev1.Container{{Name: "main"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
				{Name: "main", Ready: false, RestartCount: 3, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "default", CreationTimestamp: metav1.NewTime(t0.Add(-time.Hour))},
			Spec:       corev1.PodSpec{NodeName: "lucidbench-control-plane"},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
		job("done", batchv1.JobComplete, 0),
		job("broke", batchv1.JobFailed, 0),
		job("busy", "", 1),
	}
}

func TestReads(t *testing.T) {
	c := &Client{CS: fake.NewSimpleClientset(objects()...)}
	ctx := context.Background()

	ns, err := c.Namespaces(ctx)
	if err != nil || fmt.Sprint(ns) != "[default lucidbench]" {
		t.Fatalf("namespaces = %v, %v", ns, err)
	}
	nodes, err := c.Nodes(ctx)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %v, %v", nodes, err)
	}
	if n := nodes[0]; !n.Ready || n.Pods != 1 || n.CPU != "8" || fmt.Sprint(n.Roles) != "[control-plane]" || len(n.Conditions) != 0 {
		t.Errorf("node = %+v", n)
	}
	pods, err := c.Pods(ctx, "lucidbench")
	if err != nil || len(pods) != 1 {
		t.Fatalf("pods = %v, %v", pods, err)
	}
	if p := pods[0]; p.Restarts != 3 || p.Ready != "0/1" || p.Reason != "CrashLoopBackOff" || p.Owner != "Job/hello-1" {
		t.Errorf("pod = %+v", p)
	}
	all, _ := c.Pods(ctx, "")
	if len(all) != 2 || all[0].Name != "hello-1-abc" {
		t.Errorf("all pods newest first = %+v", all)
	}
	jobs, err := c.Jobs(ctx, "lucidbench")
	if err != nil || len(jobs) != 3 {
		t.Fatalf("jobs = %v, %v", jobs, err)
	}
	got := map[string]string{}
	for _, j := range jobs {
		got[j.Name] = fmt.Sprintf("%s %v", j.Status, j.Finished)
	}
	if got["done"] != "Completed true" || got["broke"] != "Failed true" || got["busy"] != "Running false" {
		t.Errorf("job states = %v", got)
	}
}

func TestEventsNewestFirstAndLimited(t *testing.T) {
	var objs []runtime.Object
	for i := range 60 {
		objs = append(objs, &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: fmt.Sprintf("e%d", i), Namespace: "lucidbench"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "p"},
			Reason:         "Pulled",
			LastTimestamp:  metav1.NewTime(t0.Add(time.Duration(i) * time.Minute)),
		})
	}
	c := &Client{CS: fake.NewSimpleClientset(objs...)}
	ev, err := c.Events(context.Background(), "lucidbench", EventLimit)
	if err != nil || len(ev) != 50 {
		t.Fatalf("events = %d, %v", len(ev), err)
	}
	if !ev[0].At.Equal(t0.Add(59*time.Minute)) || ev[0].Object != "Pod/p" {
		t.Errorf("first event = %+v", ev[0])
	}
}

func TestDeleteOnlyFinishedJobs(t *testing.T) {
	cs := fake.NewSimpleClientset(objects()...)
	c := &Client{CS: cs}
	ctx := context.Background()
	if err := c.DeleteJob(ctx, "lucidbench", "busy"); !errors.Is(err, ErrNotFinished) {
		t.Errorf("delete running = %v", err)
	}
	if err := c.DeleteJob(ctx, "lucidbench", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing = %v", err)
	}
	for _, n := range []string{"done", "broke"} {
		if err := c.DeleteJob(ctx, "lucidbench", n); err != nil {
			t.Errorf("delete %s = %v", n, err)
		}
	}
	jobs, _ := c.Jobs(ctx, "lucidbench")
	if len(jobs) != 1 || jobs[0].Name != "busy" {
		t.Errorf("left = %+v", jobs)
	}
}

func TestHTTP(t *testing.T) {
	cs := fake.NewSimpleClientset(objects()...)
	mux := http.NewServeMux()
	Register(mux, &Service{Connect: func() (kubernetes.Interface, error) { return cs, nil }})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/k8s/pods?namespace=lucidbench", nil))
	var pods []Pod
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &pods) != nil || len(pods) != 1 {
		t.Fatalf("pods: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/k8s/pods?namespace=Bad_NS", nil))
	if rec.Code != 400 {
		t.Errorf("bad namespace = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/k8s/jobs/lucidbench/done", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("delete without confirm = %d", rec.Code)
	}
	for path, code := range map[string]int{
		"/api/k8s/jobs/lucidbench/busy": http.StatusConflict,
		"/api/k8s/jobs/lucidbench/done": http.StatusOK,
		"/api/k8s/jobs/lucidbench/gone": http.StatusNotFound,
	} {
		req := httptest.NewRequest("DELETE", path, nil)
		req.Header.Set("X-Lucid-Confirm", "yes")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != code {
			t.Errorf("DELETE %s = %d, want %d", path, rec.Code, code)
		}
	}
}

func TestHTTPNoCluster(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, &Service{Connect: func() (kubernetes.Interface, error) { return nil, ErrNoCluster }})
	for _, p := range []string{"/api/k8s/namespaces", "/api/k8s/nodes", "/api/k8s/pods", "/api/k8s/jobs", "/api/k8s/events"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s = %d, want 503", p, rec.Code)
		}
	}
}
