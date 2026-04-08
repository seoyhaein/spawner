package imp

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestObserveWorkload_FindsOwnedWorkload(t *testing.T) {
	scheme := runtime.NewScheme()
	workload := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kueue.x-k8s.io/v1beta2",
			"kind":       "Workload",
			"metadata": map[string]interface{}{
				"name":      "job-run-1-abcde",
				"namespace": "default",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"kind": "Job",
						"name": "run-1",
					},
				},
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "QuotaReserved",
						"status":  "False",
						"message": "waiting for quota",
					},
					map[string]interface{}{
						"type":   "Admitted",
						"status": "False",
					},
				},
			},
		},
	}

	obs := &K8sObserver{
		ns:        "default",
		clientset: k8sfake.NewSimpleClientset(),
		dynamic:   fake.NewSimpleDynamicClient(scheme, workload),
	}

	got, err := obs.ObserveWorkload(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ObserveWorkload: %v", err)
	}
	if got.WorkloadName != "job-run-1-abcde" {
		t.Fatalf("unexpected workload name: %q", got.WorkloadName)
	}
	if got.QuotaReserved {
		t.Fatal("expected QuotaReserved=false")
	}
	if got.PendingReason != "waiting for quota" {
		t.Fatalf("unexpected pending reason: %q", got.PendingReason)
	}
}

func TestObservePod_ReportsUnschedulable(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "run-1-pod",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "run-1"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Message: "0/1 nodes are available",
				},
			},
		},
	}
	obs := &K8sObserver{
		ns:        "default",
		clientset: k8sfake.NewSimpleClientset(pod),
		dynamic:   fake.NewSimpleDynamicClient(runtime.NewScheme()),
	}

	got, err := obs.ObservePod(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ObservePod: %v", err)
	}
	if got.Scheduled {
		t.Fatal("expected Scheduled=false")
	}
	if got.UnschedulableReason == "" {
		t.Fatal("expected unschedulable reason to be populated")
	}
}

func TestParseWorkloadObservation_ParsesConditions(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "QuotaReserved",
					"status":  "False",
					"message": "quota pending",
				},
				map[string]interface{}{
					"type":   "Admitted",
					"status": "True",
				},
			},
		},
	}

	got := parseWorkloadObservation("wl-1", obj)
	if got.WorkloadName != "wl-1" {
		t.Fatalf("unexpected workload name: %q", got.WorkloadName)
	}
	if got.QuotaReserved {
		t.Fatal("expected QuotaReserved=false")
	}
	if !got.Admitted {
		t.Fatal("expected Admitted=true")
	}
	if got.PendingReason != "quota pending" {
		t.Fatalf("unexpected pending reason: %q", got.PendingReason)
	}
}

func TestUnstructuredOwnerRefs_InvalidInput(t *testing.T) {
	if refs, ok := unstructuredOwnerRefs(map[string]interface{}{}); ok || refs != nil {
		t.Fatal("expected invalid input to return nil,false")
	}
}
