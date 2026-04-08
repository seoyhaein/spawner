package imp

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// WorkloadObservation holds the Kueue admission status for a Job's workload.
//
// Kueue creates a Workload object for each K8s Job that carries queue-name
// labels. Admission is the Kueue-controlled gate; once admitted, the job
// is unsuspended and handed to kube-scheduler.
type WorkloadObservation struct {
	// WorkloadName is the name of the Kueue Workload object (e.g., "job-myrun-abc12").
	WorkloadName string
	// QuotaReserved: true = Kueue reserved quota; false = pending in queue.
	// Corresponds to workload.status.conditions[type=QuotaReserved].
	QuotaReserved bool
	// Admitted: true = workload fully admitted; false = still pending.
	// Corresponds to workload.status.conditions[type=Admitted].
	Admitted bool
	// PendingReason is the message from the QuotaReserved condition when false.
	// Example: "insufficient cpu quota" or "ClusterQueue poc-standard-cq is inactive".
	PendingReason string
}

// PodSchedulingObservation holds the kube-scheduler outcome for a Job's pod.
//
// After Kueue admits a workload, kube-scheduler places the pod on a node.
// If placement fails (node affinity, taint, resource constraints at node level),
// the pod stays in Pending with PodScheduled=False, reason=Unschedulable.
// This is DISTINCT from Kueue pending (quota not yet reserved).
type PodSchedulingObservation struct {
	// PodName is the name of the Job's first pod.
	PodName string
	// Scheduled: true = pod was placed on a node; false = unschedulable.
	Scheduled bool
	// Reason is the message from the PodScheduled condition when false.
	// Example: "0/1 nodes are available: 1 node(s) didn't match node selector"
	UnschedulableReason string
}

// K8sObserver provides Kueue and kube-scheduler observation paths.
// These paths are SEPARATE from the Job completion path used by DriverK8s.Wait.
//
// Why two separate observation paths:
//   - DriverK8s.Wait: watches Job conditions (Complete / Failed).
//   - K8sObserver.ObserveWorkload: watches Kueue Workload admission.
//   - K8sObserver.ObservePod: watches Pod scheduling by kube-scheduler.
//
// Kueue pending ≠ kube-scheduler unschedulable:
//   - Kueue pending: quota not yet available  → Workload.QuotaReserved=False
//   - kube-scheduler unschedulable: node mismatch → Pod.PodScheduled=False
//
// ASSUMPTION: production monitors both paths independently and surfaces them
// as distinct event types to operators/users.
type K8sObserver struct {
	ns        string
	clientset kubernetes.Interface
	dynamic   dynamic.Interface
}

var kueueWorkloadGVR = schema.GroupVersionResource{
	Group:    "kueue.x-k8s.io",
	Version:  "v1beta2",
	Resource: "workloads",
}

// buildConfig resolves a *rest.Config from the given kubeconfig path.
// When path is "", it tries in-cluster config first, then falls back to the
// default kubeconfig (KUBECONFIG env var → ~/.kube/config).
func buildConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	// Try in-cluster first (running inside a pod)
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	// Fall back: use the same loading rules kubectl uses (KUBECONFIG env var,
	// then ~/.kube/config). BuildConfigFromFlags("","") does NOT read KUBECONFIG
	// env var reliably; use the loading rules API directly.
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	return clientConfig.ClientConfig()
}

// NewK8sObserverFromKubeconfig creates a K8sObserver using the given kubeconfig path.
// Pass "" to try in-cluster config first, then fall back to default kubeconfig.
func NewK8sObserverFromKubeconfig(ns, kubeconfigPath string) (*K8sObserver, error) {
	cfg, err := buildConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("k8s config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return &K8sObserver{ns: ns, clientset: cs, dynamic: dyn}, nil
}

// ObserveWorkload finds the Kueue Workload for the given Job and returns
// its admission status.
//
// Kueue workload names follow the pattern: "job-{job-name}-{5chars}".
// This method searches by ownerReference to find the workload owned by the Job.
func (o *K8sObserver) ObserveWorkload(ctx context.Context, jobName string) (WorkloadObservation, error) {
	list, err := o.dynamic.Resource(kueueWorkloadGVR).Namespace(o.ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return WorkloadObservation{}, fmt.Errorf("list workloads: %w", err)
	}

	for _, item := range list.Items {
		// Match by ownerReference: Job with same name
		ownerRefs, ok := unstructuredOwnerRefs(item.Object)
		if !ok {
			continue
		}
		for _, ref := range ownerRefs {
			if ref["kind"] == "Job" && ref["name"] == jobName {
				return parseWorkloadObservation(item.GetName(), item.Object), nil
			}
		}
	}
	return WorkloadObservation{}, fmt.Errorf("no workload found for job %s", jobName)
}

// ObservePod finds the first Pod created by the given Job and returns its
// scheduling status from kube-scheduler.
func (o *K8sObserver) ObservePod(ctx context.Context, jobName string) (PodSchedulingObservation, error) {
	pods, err := o.clientset.CoreV1().Pods(o.ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return PodSchedulingObservation{}, fmt.Errorf("list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return PodSchedulingObservation{}, fmt.Errorf("no pods found for job %s", jobName)
	}
	pod := pods.Items[0]
	obs := PodSchedulingObservation{PodName: pod.Name, Scheduled: true}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			obs.Scheduled = false
			obs.UnschedulableReason = cond.Message
		}
	}
	return obs, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func unstructuredOwnerRefs(obj map[string]interface{}) ([]map[string]interface{}, bool) {
	meta, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	raw, ok := meta["ownerReferences"]
	if !ok {
		return nil, false
	}
	refs, ok := raw.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]map[string]interface{}, 0, len(refs))
	for _, r := range refs {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, true
}

func parseWorkloadObservation(name string, obj map[string]interface{}) WorkloadObservation {
	obs := WorkloadObservation{WorkloadName: name}
	status, _ := obj["status"].(map[string]interface{})
	if status == nil {
		return obs
	}
	conditions, _ := status["conditions"].([]interface{})
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		condMsg, _ := cond["message"].(string)
		switch condType {
		case "QuotaReserved":
			obs.QuotaReserved = condStatus == "True"
			if condStatus == "False" {
				obs.PendingReason = condMsg
			}
		case "Admitted":
			obs.Admitted = condStatus == "True"
		}
	}
	return obs
}
