package kueue

import (
	"testing"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	"k8s.io/utils/ptr"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// ---------------------------------------------------------------------------
// topology disabled → no topology request emitted
// ---------------------------------------------------------------------------

func TestPodSetsTopologyNilNoTopologyRequest(t *testing.T) {
	rtj := testRTJForPodSets(t) // topology is nil by default
	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}
	for _, ps := range podSets {
		if ps.TopologyRequest != nil {
			t.Fatalf("expected no TopologyRequest on pod set %q when topology is nil, got %+v", ps.Name, ps.TopologyRequest)
		}
	}
}

func TestPodSetsTopologyExplicitlyDisabledNoTopologyRequest(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode: trainingv1alpha1.TopologyModeDisabled,
	}
	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}
	for _, ps := range podSets {
		if ps.TopologyRequest != nil {
			t.Fatalf("expected no TopologyRequest on pod set %q when mode is Disabled, got %+v", ps.Name, ps.TopologyRequest)
		}
	}
}

// ---------------------------------------------------------------------------
// required topology emitted correctly
// ---------------------------------------------------------------------------

func TestPodSetsTopologyRequiredEmitsRequired(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:          trainingv1alpha1.TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PodSetName: "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Driver should NOT have topology (colocation is false).
	driver := mustFindPodSet(t, podSets, "driver")
	if driver.TopologyRequest != nil {
		t.Fatalf("expected no TopologyRequest on driver (colocation disabled), got %+v", driver.TopologyRequest)
	}

	// Worker should have Required topology.
	worker := mustFindPodSet(t, podSets, "worker")
	if worker.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on worker")
	}
	if worker.TopologyRequest.Required == nil || *worker.TopologyRequest.Required != "topology.kubernetes.io/zone" {
		t.Fatalf("expected Required=topology.kubernetes.io/zone, got %v", worker.TopologyRequest.Required)
	}
	if worker.TopologyRequest.Preferred != nil {
		t.Fatalf("expected Preferred to be nil, got %v", worker.TopologyRequest.Preferred)
	}
	if worker.TopologyRequest.Unconstrained != nil {
		t.Fatalf("expected Unconstrained to be nil, got %v", worker.TopologyRequest.Unconstrained)
	}
	assertSubGroupMetadata(t, worker.TopologyRequest, 2) // worker replicatedJob has replicas=2
	if worker.TopologyRequest.PodSetGroupName != nil {
		t.Fatalf("expected no PodSetGroupName (colocation disabled), got %v", *worker.TopologyRequest.PodSetGroupName)
	}
}

// ---------------------------------------------------------------------------
// preferred topology emitted correctly
// ---------------------------------------------------------------------------

func TestPodSetsTopologyPreferredEmitsPreferred(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:          trainingv1alpha1.TopologyModePreferred,
		TopologyLevel: "cloud.provider.com/topology-rack",
	}
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PodSetName: "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	worker := mustFindPodSet(t, podSets, "worker")
	if worker.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on worker")
	}
	if worker.TopologyRequest.Preferred == nil || *worker.TopologyRequest.Preferred != "cloud.provider.com/topology-rack" {
		t.Fatalf("expected Preferred=cloud.provider.com/topology-rack, got %v", worker.TopologyRequest.Preferred)
	}
	if worker.TopologyRequest.Required != nil {
		t.Fatalf("expected Required to be nil, got %v", worker.TopologyRequest.Required)
	}
	if worker.TopologyRequest.Unconstrained != nil {
		t.Fatalf("expected Unconstrained to be nil, got %v", worker.TopologyRequest.Unconstrained)
	}
	assertSubGroupMetadata(t, worker.TopologyRequest, 2)
}

// ---------------------------------------------------------------------------
// unconstrained topology emitted correctly
// ---------------------------------------------------------------------------

func TestPodSetsTopologyUnconstrainedEmitsUnconstrained(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode: trainingv1alpha1.TopologyModeUnconstrained,
	}
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PodSetName: "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	worker := mustFindPodSet(t, podSets, "worker")
	if worker.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on worker")
	}
	if worker.TopologyRequest.Unconstrained == nil || !*worker.TopologyRequest.Unconstrained {
		t.Fatalf("expected Unconstrained=true, got %v", worker.TopologyRequest.Unconstrained)
	}
	if worker.TopologyRequest.Required != nil {
		t.Fatalf("expected Required to be nil, got %v", worker.TopologyRequest.Required)
	}
	if worker.TopologyRequest.Preferred != nil {
		t.Fatalf("expected Preferred to be nil, got %v", worker.TopologyRequest.Preferred)
	}
	assertSubGroupMetadata(t, worker.TopologyRequest, 2)
}

// ---------------------------------------------------------------------------
// leader/worker colocation grouping semantics
// ---------------------------------------------------------------------------

func TestPodSetsTopologyColocationAppliesBothPodSets(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:                   trainingv1alpha1.TopologyModeRequired,
		TopologyLevel:          "topology.kubernetes.io/zone",
		LeaderWorkerColocation: true,
	}
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PodSetName: "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Driver should have topology when colocation is true.
	driver := mustFindPodSet(t, podSets, "driver")
	if driver.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on driver when colocation is true")
	}
	if driver.TopologyRequest.Required == nil || *driver.TopologyRequest.Required != "topology.kubernetes.io/zone" {
		t.Fatalf("expected driver Required=topology.kubernetes.io/zone, got %v", driver.TopologyRequest.Required)
	}
	if driver.TopologyRequest.PodSetGroupName == nil || *driver.TopologyRequest.PodSetGroupName != topologyColocationGroup {
		t.Fatalf("expected driver PodSetGroupName=%s, got %v", topologyColocationGroup, driver.TopologyRequest.PodSetGroupName)
	}
	assertSubGroupMetadata(t, driver.TopologyRequest, 1) // driver replicatedJob has replicas=1

	// Worker should also have topology and same group name.
	worker := mustFindPodSet(t, podSets, "worker")
	if worker.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on worker")
	}
	if worker.TopologyRequest.Required == nil || *worker.TopologyRequest.Required != "topology.kubernetes.io/zone" {
		t.Fatalf("expected worker Required=topology.kubernetes.io/zone, got %v", worker.TopologyRequest.Required)
	}
	if worker.TopologyRequest.PodSetGroupName == nil || *worker.TopologyRequest.PodSetGroupName != topologyColocationGroup {
		t.Fatalf("expected worker PodSetGroupName=%s, got %v", topologyColocationGroup, worker.TopologyRequest.PodSetGroupName)
	}
	assertSubGroupMetadata(t, worker.TopologyRequest, 2)
}

func TestPodSetsTopologyNoColocationOnlyWorkerGetsTopology(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:                   trainingv1alpha1.TopologyModeRequired,
		TopologyLevel:          "topology.kubernetes.io/zone",
		LeaderWorkerColocation: false,
	}
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PodSetName: "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	driver := mustFindPodSet(t, podSets, "driver")
	if driver.TopologyRequest != nil {
		t.Fatalf("expected no TopologyRequest on driver when colocation is false, got %+v", driver.TopologyRequest)
	}

	worker := mustFindPodSet(t, podSets, "worker")
	if worker.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on worker")
	}
	if worker.TopologyRequest.PodSetGroupName != nil {
		t.Fatalf("expected no PodSetGroupName when colocation is false, got %v", *worker.TopologyRequest.PodSetGroupName)
	}
}

func TestPodSetsTopologyColocationPreferredMode(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:                   trainingv1alpha1.TopologyModePreferred,
		TopologyLevel:          "cloud.provider.com/topology-rack",
		LeaderWorkerColocation: true,
	}
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PodSetName: "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Both PodSets should have Preferred topology with colocation group.
	for _, name := range []string{"driver", "worker"} {
		ps := mustFindPodSet(t, podSets, name)
		if ps.TopologyRequest == nil {
			t.Fatalf("expected TopologyRequest on %s", name)
		}
		if ps.TopologyRequest.Preferred == nil || *ps.TopologyRequest.Preferred != "cloud.provider.com/topology-rack" {
			t.Fatalf("expected %s Preferred=cloud.provider.com/topology-rack, got %v", name, ps.TopologyRequest.Preferred)
		}
		if ps.TopologyRequest.PodSetGroupName == nil || *ps.TopologyRequest.PodSetGroupName != topologyColocationGroup {
			t.Fatalf("expected %s PodSetGroupName=%s, got %v", name, topologyColocationGroup, ps.TopologyRequest.PodSetGroupName)
		}
	}
}

// ---------------------------------------------------------------------------
// default worker resolution: topology without explicit podSetName
// ---------------------------------------------------------------------------

func TestPodSetsTopologyDefaultWorkerIsFirstReplicatedJob(t *testing.T) {
	rtj := testRTJForPodSets(t)
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:          trainingv1alpha1.TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	// No parallelism.podSetName → defaults to first replicatedJob ("driver").

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// First replicatedJob ("driver") is treated as worker for topology.
	driver := mustFindPodSet(t, podSets, "driver")
	if driver.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on driver (default worker)")
	}

	// Second replicatedJob ("worker") should NOT have topology.
	worker := mustFindPodSet(t, podSets, "worker")
	if worker.TopologyRequest != nil {
		t.Fatalf("expected no TopologyRequest on worker (not the resolved default worker), got %+v", worker.TopologyRequest)
	}
}

// ---------------------------------------------------------------------------
// Phase 3 behavior preserved when topology is added
// ---------------------------------------------------------------------------

func TestPodSetsTopologyPreservesPhase3CountsAndMinCount(t *testing.T) {
	SetExperimentalPartialAdmission(true)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Resume.AllowWorldSizeChange = true
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "worker",
		EnablePartialAdmission: true,
	}
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:          trainingv1alpha1.TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Phase 3 behavior: driver count unchanged, no MinCount.
	driver := mustFindPodSet(t, podSets, "driver")
	if driver.Count != 1 {
		t.Fatalf("expected driver count=1, got %d", driver.Count)
	}
	if driver.MinCount != nil {
		t.Fatalf("expected no MinCount on driver, got %d", *driver.MinCount)
	}

	// Phase 3 behavior: worker count=preferredCount, MinCount from parallelism.
	worker := mustFindPodSet(t, podSets, "worker")
	if worker.Count != 8 {
		t.Fatalf("expected worker count=8 (preferredCount), got %d", worker.Count)
	}
	if worker.MinCount == nil || *worker.MinCount != 4 {
		t.Fatalf("expected worker MinCount=4, got %v", worker.MinCount)
	}

	// Phase 4 topology also present on worker.
	if worker.TopologyRequest == nil {
		t.Fatal("expected TopologyRequest on worker")
	}
	if worker.TopologyRequest.Required == nil || *worker.TopologyRequest.Required != "topology.kubernetes.io/zone" {
		t.Fatalf("expected Required=topology.kubernetes.io/zone, got %v", worker.TopologyRequest.Required)
	}
}

func TestPodSetsTopologyDisabledPreservesPhase3ExactBehavior(t *testing.T) {
	SetExperimentalPartialAdmission(true)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Resume.AllowWorldSizeChange = true
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "worker",
		EnablePartialAdmission: true,
	}
	// Topology disabled explicitly.
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode: trainingv1alpha1.TopologyModeDisabled,
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Phase 3 counts preserved.
	worker := mustFindPodSet(t, podSets, "worker")
	if worker.Count != 8 {
		t.Fatalf("expected worker count=8, got %d", worker.Count)
	}
	if worker.MinCount == nil || *worker.MinCount != 4 {
		t.Fatalf("expected worker MinCount=4, got %v", worker.MinCount)
	}
	// No topology.
	if worker.TopologyRequest != nil {
		t.Fatalf("expected no TopologyRequest when mode is Disabled, got %+v", worker.TopologyRequest)
	}
}

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

func mustFindPodSet(t *testing.T, podSets []kueuev1beta2.PodSet, name string) kueuev1beta2.PodSet {
	t.Helper()
	for _, ps := range podSets {
		if string(ps.Name) == name {
			return ps
		}
	}
	t.Fatalf("pod set %q not found in %d pod sets", name, len(podSets))
	return kueuev1beta2.PodSet{} // unreachable
}

func assertSubGroupMetadata(t *testing.T, req *kueuev1beta2.PodSetTopologyRequest, expectedReplicas int32) {
	t.Helper()
	if req.PodIndexLabel == nil || *req.PodIndexLabel != JobCompletionIndexLabel {
		t.Fatalf("expected PodIndexLabel=%s, got %v", JobCompletionIndexLabel, req.PodIndexLabel)
	}
	if req.SubGroupIndexLabel == nil || *req.SubGroupIndexLabel != JobSetJobIndexLabel {
		t.Fatalf("expected SubGroupIndexLabel=%s, got %v", JobSetJobIndexLabel, req.SubGroupIndexLabel)
	}
	if req.SubGroupCount == nil || *req.SubGroupCount != expectedReplicas {
		t.Fatalf("expected SubGroupCount=%d, got %v", expectedReplicas, req.SubGroupCount)
	}
}
