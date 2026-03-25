package jobset

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	kueueconstants "sigs.k8s.io/kueue/pkg/controller/constants"
)

func TestApplyAdmittedReplicaCountSinglePodPerReplica(t *testing.T) {
	rj := makeReplicatedJob("trainer", 1, 1, 1)
	applyAdmittedReplicaCount(rj, 4)
	if got := ptr.Deref(rj.Replicas, 0); got != 4 {
		t.Fatalf("expected 4 replicas, got %d", got)
	}
}

func TestApplyAdmittedReplicaCountMultiPodsPerReplica(t *testing.T) {
	// 2 pods per replica (parallelism=2, completions=2): admitting 8 pods → 4 replicas.
	rj := makeReplicatedJob("trainer", 1, 2, 2)
	applyAdmittedReplicaCount(rj, 8)
	if got := ptr.Deref(rj.Replicas, 0); got != 4 {
		t.Fatalf("expected 4 replicas, got %d", got)
	}
}

func TestApplyAdmittedReplicaCountZeroCountIsNoOp(t *testing.T) {
	rj := makeReplicatedJob("trainer", 2, 1, 1)
	applyAdmittedReplicaCount(rj, 0)
	if got := ptr.Deref(rj.Replicas, 0); got != 2 {
		t.Fatalf("expected original 2 replicas to be preserved, got %d", got)
	}
}

func TestApplyAdmittedReplicaCountNegativeCountIsNoOp(t *testing.T) {
	rj := makeReplicatedJob("trainer", 3, 1, 1)
	applyAdmittedReplicaCount(rj, -1)
	if got := ptr.Deref(rj.Replicas, 0); got != 3 {
		t.Fatalf("expected original 3 replicas to be preserved, got %d", got)
	}
}

func TestApplyAdmittedReplicaCountClampsToAtLeastOne(t *testing.T) {
	// parallelism=4, admittedCount=1 → floor(1/4)=0 → clamped to 1.
	rj := makeReplicatedJob("trainer", 2, 4, 4)
	applyAdmittedReplicaCount(rj, 1)
	if got := ptr.Deref(rj.Replicas, 0); got != 1 {
		t.Fatalf("expected 1 replica (clamped), got %d", got)
	}
}

func TestPodsPerReplicaDefaultsToOne(t *testing.T) {
	rj := &ReplicatedJob{
		Name: "trainer",
		Template: batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{},
		},
	}
	if got := podsPerReplica(rj); got != 1 {
		t.Fatalf("expected 1 pod per replica for defaults, got %d", got)
	}
}

func TestPodsPerReplicaUsesParallelism(t *testing.T) {
	rj := makeReplicatedJob("trainer", 1, 4, 4)
	if got := podsPerReplica(rj); got != 4 {
		t.Fatalf("expected 4 pods per replica, got %d", got)
	}
}

func TestPodsPerReplicaCompletionsLessThanParallelism(t *testing.T) {
	rj := makeReplicatedJob("trainer", 1, 4, 2)
	if got := podsPerReplica(rj); got != 2 {
		t.Fatalf("expected 2 pods per replica (capped by completions), got %d", got)
	}
}

func TestStripKueuePodTemplateLabelsRemovesKueueLabels(t *testing.T) {
	rj := makeReplicatedJob("trainer", 1, 1, 1)
	rj.Template.Spec.Template.ObjectMeta = metav1.ObjectMeta{
		Labels: map[string]string{
			"app":                                "counter",
			kueueconstants.QueueLabel:             "queue-a",
			"kueue.x-k8s.io/managed":             "true",
			kueueconstants.PrebuiltWorkloadLabel:  "pre-built",
		},
		Annotations: map[string]string{
			"example.com/keep":                                    "yes",
			kueueconstants.ProvReqAnnotationPrefix + "flavor":     "gpu-a",
		},
	}

	stripKueuePodTemplateLabels(rj)

	meta := rj.Template.Spec.Template.ObjectMeta
	if _, found := meta.Labels[kueueconstants.QueueLabel]; found {
		t.Fatalf("expected queue label to be stripped from pod template")
	}
	if _, found := meta.Labels["kueue.x-k8s.io/managed"]; found {
		t.Fatalf("expected kueue.x-k8s.io/ prefixed label to be stripped")
	}
	if _, found := meta.Labels[kueueconstants.PrebuiltWorkloadLabel]; found {
		t.Fatalf("expected prebuilt workload label to be stripped")
	}
	if got := meta.Labels["app"]; got != "counter" {
		t.Fatalf("expected non-Kueue label to be preserved, got %q", got)
	}
	if _, found := meta.Annotations[kueueconstants.ProvReqAnnotationPrefix+"flavor"]; found {
		t.Fatalf("expected provisioning annotation to be stripped from pod template")
	}
	if got := meta.Annotations["example.com/keep"]; got != "yes" {
		t.Fatalf("expected non-Kueue annotation to be preserved, got %q", got)
	}
}

func TestStripKueuePodTemplateLabelsNilMapsNoOp(t *testing.T) {
	rj := makeReplicatedJob("trainer", 1, 1, 1)
	// Labels and Annotations are nil by default.
	stripKueuePodTemplateLabels(rj)
	// Should not panic.
}

func makeReplicatedJob(name string, replicas, parallelism, completions int32) *ReplicatedJob {
	return &ReplicatedJob{
		Name:     name,
		Replicas: ptr.To(replicas),
		Template: batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{
				Parallelism: ptr.To(parallelism),
				Completions: ptr.To(completions),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{Name: "trainer", Image: "counter:latest"},
						},
					},
				},
			},
		},
	}
}
