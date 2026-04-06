SHELL := /bin/bash

KIND_CLUSTER_NAME ?= checkpoint-phase1
DEV_NAMESPACE ?= checkpoint-dev
IMAGES ?= controller:latest
EXAMPLE_RTJ_NAME ?= phase1-demo
EXAMPLE_TRAINER_IMAGE ?=
PAUSE_FLOW_TRAINER_IMAGE ?= $(EXAMPLE_TRAINER_IMAGE)
PHASE2_TRAINER_IMAGE ?= $(PAUSE_FLOW_TRAINER_IMAGE)
PHASE2_LOW_RTJ_NAME ?= phase2-low
PHASE2_HIGH_RTJ_NAME ?= phase2-high
RTJ_NAME ?= $(PHASE2_LOW_RTJ_NAME)
LOCAL_QUEUE_NAME ?= training
PHASE3_PROFILE ?= flavors
PHASE3_RTJ_NAME ?= phase3-demo
PHASE3_TRAINER_IMAGE ?= $(PHASE2_TRAINER_IMAGE)
PHASE4_RTJ_NAME ?= phase4-demo
PHASE4_TRAINER_IMAGE ?= $(PHASE3_TRAINER_IMAGE)
PHASE5_LOW_RTJ_NAME ?= phase5-low-demo
PHASE5_HIGH_RTJ_NAME ?= phase5-high-demo
PHASE5_TRAINER_IMAGE ?= $(PHASE4_TRAINER_IMAGE)
PHASE6_MANAGER ?= phase6-manager
PHASE6_WORKER_1 ?= phase6-worker-1
PHASE6_WORKER_2 ?= phase6-worker-2
PHASE6_RTJ_NAME ?= phase6-dispatch-demo
PHASE6_TRAINER_IMAGE ?= $(PHASE5_TRAINER_IMAGE)

.PHONY: dev-up dev-down dev-status dev-smoke phase2-smoke load-images submit-example pause-example resume-example inspect-example submit-low submit-high inspect-rtj inspect-kueue e2e e2e-phase2
.PHONY: phase3-up phase3-down phase3-status phase3-load-images phase3-smoke phase3-profile e2e-phase3
.PHONY: phase3-submit-flavor phase3-submit-flex phase3-inspect-admission phase3-inspect-checkpoints
.PHONY: phase4-up phase4-down phase4-status phase4-load-images phase4-smoke
.PHONY: phase4-submit-topology phase4-submit-gated-resume
.PHONY: phase4-inspect-workload phase4-inspect-admissioncheck phase4-inspect-topology phase4-inspect-checkpoints
.PHONY: e2e-phase4
.PHONY: phase5-up phase5-down phase5-status phase5-load-images phase5-smoke phase5-profile
.PHONY: phase5-submit-low phase5-submit-high
.PHONY: phase5-inspect-priority phase5-inspect-policy phase5-inspect-workload phase5-inspect-checkpoints
.PHONY: e2e-phase5
.PHONY: phase6-up phase6-down phase6-status phase6-load-images phase6-smoke
.PHONY: phase6-submit phase6-pause phase6-resume
.PHONY: phase6-inspect-manager phase6-inspect-worker phase6-inspect-checkpoints
.PHONY: e2e-phase6
.PHONY: phase7-up phase7-down phase7-status phase7-load-images phase7-smoke phase7-profile
.PHONY: phase7-build-fake-provisioner e2e-phase7
.PHONY: phase7-submit-success phase7-submit-fail
.PHONY: phase7-inspect-launchgate phase7-inspect-workload phase7-inspect-provisioningrequest phase7-inspect-checkpoints
.PHONY: phase8-up phase8-down phase8-status phase8-load-images phase8-smoke phase8-profile
.PHONY: phase8-submit phase8-pause phase8-resume
.PHONY: phase8-inspect-dra phase8-inspect-kueue phase8-inspect-checkpoints
.PHONY: e2e-phase8
.PHONY: phase9-up phase9-down phase9-status phase9-load-images phase9-smoke phase9-profile

dev-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-up.sh

dev-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

dev-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh

dev-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/smoke.sh

phase2-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase2-smoke.sh

load-images:
	@set -euo pipefail; \
	for image in $(IMAGES); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

submit-example:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) EXAMPLE_RTJ_NAME=$(EXAMPLE_RTJ_NAME) EXAMPLE_TRAINER_IMAGE=$(EXAMPLE_TRAINER_IMAGE) ./hack/dev/submit-example.sh

pause-example:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) EXAMPLE_RTJ_NAME=$(EXAMPLE_RTJ_NAME) ./hack/dev/pause-example.sh

resume-example:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) EXAMPLE_RTJ_NAME=$(EXAMPLE_RTJ_NAME) ./hack/dev/resume-example.sh

inspect-example:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) EXAMPLE_RTJ_NAME=$(EXAMPLE_RTJ_NAME) ./hack/dev/inspect-example.sh

submit-low:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE2_LOW_RTJ_NAME=$(PHASE2_LOW_RTJ_NAME) PHASE2_TRAINER_IMAGE=$(PHASE2_TRAINER_IMAGE) ./hack/dev/submit-low-priority.sh

submit-high:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE2_HIGH_RTJ_NAME=$(PHASE2_HIGH_RTJ_NAME) PHASE2_TRAINER_IMAGE=$(PHASE2_TRAINER_IMAGE) ./hack/dev/submit-high-priority.sh

inspect-rtj:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE2_LOW_RTJ_NAME=$(PHASE2_LOW_RTJ_NAME) RTJ_NAME=$(RTJ_NAME) ./hack/dev/inspect-rtj.sh

inspect-kueue:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) LOCAL_QUEUE_NAME=$(LOCAL_QUEUE_NAME) ./hack/dev/inspect-kueue.sh

e2e:
	RUN_KIND_E2E=1 PAUSE_FLOW_TRAINER_IMAGE=$(PAUSE_FLOW_TRAINER_IMAGE) go test ./test/e2e -v

e2e-phase2:
	RUN_KIND_E2E=1 PHASE2_TRAINER_IMAGE=$(PHASE2_TRAINER_IMAGE) PAUSE_FLOW_TRAINER_IMAGE=$(PAUSE_FLOW_TRAINER_IMAGE) go test ./test/e2e -run 'TestNativeKueueAdmission|TestPriorityPreemptionResume' -v

e2e-phase3:
	RUN_KIND_E2E=1 PHASE3_TRAINER_IMAGE=$(PHASE3_TRAINER_IMAGE) go test ./test/e2e -run 'TestAdmissionMaterialization|TestFlavorAwareLaunch|TestFlexibleResume' -v -timeout 20m

# ── Phase 3 targets ──────────────────────────────────────────────────
#
# phase3-up:                 Create kind cluster with 4 workers, install base
#                            stack, then apply Phase 3 flavor profile.
# phase3-down:               Delete the kind cluster.
# phase3-status:             Show cluster state including flavors and node pools.
# phase3-load-images:        Load images into the Phase 3 cluster.
# phase3-smoke:              Run Phase 3 infrastructure smoke test.
# phase3-profile:            Apply/switch Phase 3 profile on existing cluster.
#                            PHASE3_PROFILE=flavors (default) | experimental
# phase3-submit-flavor:      Submit a fixed-size RTJ on the multi-flavor queue.
# phase3-submit-flex:        Submit a flexible-size RTJ (allowWorldSizeChange).
# phase3-inspect-admission:  Inspect admission state for an RTJ.
# phase3-inspect-checkpoints: Inspect checkpoint/restore state for an RTJ.
#
# The "flavors" profile exercises G1 (admission-aware launch), G2 (flavor-aware
# rendering), and G3 (flexible-size resume). The "experimental" profile adds
# G4 (partial admission). The operator must also be started with
# --enable-experimental-partial-admission for G4.

phase3-submit-flavor:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE3_RTJ_NAME=$(PHASE3_RTJ_NAME) PHASE3_TRAINER_IMAGE=$(PHASE3_TRAINER_IMAGE) ./hack/dev/submit-flavor-example.sh

phase3-submit-flex:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE3_RTJ_NAME=$(PHASE3_RTJ_NAME) PHASE3_TRAINER_IMAGE=$(PHASE3_TRAINER_IMAGE) ./hack/dev/submit-flex-example.sh

phase3-inspect-admission:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE3_RTJ_NAME=$(PHASE3_RTJ_NAME) RTJ_NAME=$(RTJ_NAME) ./hack/dev/inspect-admission.sh

phase3-inspect-checkpoints:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE3_RTJ_NAME=$(PHASE3_RTJ_NAME) RTJ_NAME=$(RTJ_NAME) ./hack/dev/inspect-checkpoints.sh

phase3-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  KIND_CONFIG_PATH=hack/dev/kind-config-phase3.yaml \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/dev-up.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  PHASE3_PROFILE=$(PHASE3_PROFILE) \
	  ./hack/dev/phase3-profile.sh

phase3-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

phase3-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh
	@echo
	@echo "phase3 node pools:"
	@kubectl get nodes -L checkpoint-native.dev/pool -L checkpoint-native.dev/phase3 --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@echo
	@echo "phase3 resource flavors:"
	@kubectl get resourceflavors.kueue.x-k8s.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@echo
	@echo "phase3 queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true

phase3-load-images:
	@set -euo pipefail; \
	for image in $(IMAGES); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

phase3-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase3-smoke.sh

phase3-profile:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  PHASE3_PROFILE=$(PHASE3_PROFILE) \
	  ./hack/dev/phase3-profile.sh

# ── Phase 4 targets ──────────────────────────────────────────────────
#
# phase4-up:          Create kind cluster with 4 workers, install base
#                     stack, then apply Phase 4 topology + admission check
#                     profile. Reuses the Phase 3 kind config (4 workers).
# phase4-down:        Delete the kind cluster.
# phase4-status:      Show cluster state including topology, flavors, queues,
#                     admission checks, and node topology labels.
# phase4-load-images: Load images into the Phase 4 cluster.
# phase4-smoke:       Run Phase 4 infrastructure smoke test. Verifies topology
#                     labels, Kueue Topology object, ResourceFlavor,
#                     ClusterQueue with admission check, and queue health.
#
# The Phase 4 profile exercises:
#   G1: Topology-aware Workload synthesis (TopologyRequest on PodSets)
#   G2: Topology-aware runtime materialization (nodeSelector from TAS)
#   G3: ResumeReadiness AdmissionCheck controller
#   G4: Admission-gated launch and resume
#
# Sample RTJs in deploy/dev/samples/phase4/:
#   rtj-topology-disabled.yaml    — Phase 3 behavior on Phase 4 queue
#   rtj-topology-preferred.yaml   — Preferred rack-level placement
#   rtj-topology-required.yaml    — Required rack-level placement
#   rtj-resume-readiness-gated.yaml — Admission-check-gated launch

phase4-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  KIND_CONFIG_PATH=hack/dev/kind-config-phase3.yaml \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/dev-up.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/install-phase4-profile.sh

phase4-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

phase4-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh
	@echo
	@echo "phase4 node topology:"
	@kubectl get nodes \
	  -L topology.example.io/block \
	  -L topology.example.io/rack \
	  -L checkpoint-native.dev/pool \
	  --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@echo
	@echo "phase4 topology:"
	@kubectl get topologies.kueue.x-k8s.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (Topology CRD not available)"
	@echo
	@echo "phase4 resource flavors:"
	@kubectl get resourceflavors.kueue.x-k8s.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@echo
	@echo "phase4 queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || true
	@echo
	@echo "phase4 admission checks:"
	@kubectl get admissionchecks.kueue.x-k8s.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase4 resume readiness policies:"
	@kubectl get resumereadinesspolicies.training.checkpoint.example.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"

phase4-load-images:
	@set -euo pipefail; \
	for image in $(IMAGES); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

phase4-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase4-smoke.sh

# ── Phase 4 demo / inspect targets ─────────────────────────────────
#
# phase4-submit-topology:        Submit a topology-aware RTJ.
#                                 PHASE4_TOPOLOGY_MODE=required (default) | preferred
# phase4-submit-gated-resume:    Submit an RTJ that exercises the readiness gate.
# phase4-inspect-workload:       Inspect RTJ + Kueue Workload status.
# phase4-inspect-admissioncheck: Inspect ResumeReadiness AdmissionCheck + policy.
# phase4-inspect-topology:       Inspect topology assignment and node placement.
# phase4-inspect-checkpoints:    Inspect checkpoint evidence used by readiness gate.
# e2e-phase4:                    Run Phase 4 e2e tests.

PHASE4_TOPOLOGY_MODE ?= required

phase4-submit-topology:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE4_RTJ_NAME=$(PHASE4_RTJ_NAME) PHASE4_TRAINER_IMAGE=$(PHASE4_TRAINER_IMAGE) PHASE4_TOPOLOGY_MODE=$(PHASE4_TOPOLOGY_MODE) ./hack/dev/submit-topology-demo.sh

phase4-submit-gated-resume:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE4_RTJ_NAME=$(PHASE4_RTJ_NAME) PHASE4_TRAINER_IMAGE=$(PHASE4_TRAINER_IMAGE) ./hack/dev/submit-gated-resume-demo.sh

phase4-inspect-workload:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE4_RTJ_NAME=$(PHASE4_RTJ_NAME) ./hack/dev/inspect-workload.sh

phase4-inspect-admissioncheck:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/inspect-admissioncheck.sh

phase4-inspect-topology:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE4_RTJ_NAME=$(PHASE4_RTJ_NAME) ./hack/dev/inspect-topology.sh

phase4-inspect-checkpoints:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE4_RTJ_NAME=$(PHASE4_RTJ_NAME) ./hack/dev/inspect-checkpoints-phase4.sh

e2e-phase4:
	RUN_KIND_E2E=1 PHASE4_TRAINER_IMAGE=$(PHASE4_TRAINER_IMAGE) go test ./test/e2e -run 'TestResumeReadinessGate|TestTopologyAwareLaunch|TestTopologyAwareResume' -v -timeout 20m

# ── Phase 5 targets ──────────────────────────────────────────────────
#
# phase5-up:          Create kind cluster, install base stack, then apply
#                     Phase 5 checkpoint-aware priority shaping profile.
#                     Uses the default kind config (1 control-plane, 1 worker).
# phase5-down:        Delete the kind cluster.
# phase5-status:      Show cluster state including priority classes, policies,
#                     queues, and checkpoint priority policy details.
# phase5-load-images: Load images into the Phase 5 cluster.
# phase5-smoke:       Run Phase 5 infrastructure smoke test. Verifies CRDs,
#                     sample policy, preemption-capable queue, priority classes,
#                     and sample RTJ dry-run validation.
# phase5-profile:     Apply/re-apply Phase 5 profile on existing cluster.
#
# The Phase 5 profile exercises:
#   G1: Checkpoint-aware effective priority derivation
#   G2: Yield budgets / protection windows
#   G3: Effective priority written to Kueue Workload.Spec.Priority
#   G4: Deterministic within-ClusterQueue preemption profile
#
# Scope boundaries:
#   - withinClusterQueue LowerPriority preemption ONLY
#   - reclaimWithinCohort: Never (disabled)
#   - borrowWithinCohort: disabled
#   - No cohort, no Fair Sharing
#
# Sample RTJs in deploy/dev/phase5/samples/:
#   rtj-low-priority.yaml   — Low base priority (100) with priority shaping
#   rtj-high-priority.yaml  — High base priority (10000) with priority shaping

phase5-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/dev-up.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/install-phase5-profile.sh

phase5-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

phase5-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh
	@echo
	@echo "phase5 priority classes:"
	@kubectl get workloadpriorityclasses.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase5-priority-shaping --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase5 checkpoint priority policies:"
	@kubectl get checkpointprioritypolicies.training.checkpoint.example.io --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase5 queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io phase5-cq --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) phase5-training --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"

phase5-load-images:
	@set -euo pipefail; \
	for image in $(IMAGES); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

phase5-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase5-smoke.sh

phase5-profile:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase5-profile.sh

# ── Phase 5 demo / inspect targets ─────────────────────────────────
#
# phase5-submit-low:           Submit a low-priority RTJ with priority shaping.
# phase5-submit-high:          Submit a high-priority RTJ with priority shaping.
# phase5-inspect-priority:     Inspect base vs effective priority, preemption state,
#                               protection window, checkpoint freshness, yield budget.
# phase5-inspect-policy:       Inspect the CheckpointPriorityPolicy attached to an RTJ.
# phase5-inspect-workload:     Inspect RTJ + Workload status with priority shaping.
# phase5-inspect-checkpoints:  Inspect checkpoint freshness evidence for priority shaping.
# e2e-phase5:                  Run Phase 5 e2e tests.

phase5-submit-low:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE5_LOW_RTJ_NAME=$(PHASE5_LOW_RTJ_NAME) PHASE5_TRAINER_IMAGE=$(PHASE5_TRAINER_IMAGE) ./hack/dev/submit-low-priority-phase5.sh

phase5-submit-high:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE5_HIGH_RTJ_NAME=$(PHASE5_HIGH_RTJ_NAME) PHASE5_TRAINER_IMAGE=$(PHASE5_TRAINER_IMAGE) ./hack/dev/submit-high-priority-phase5.sh

phase5-inspect-priority:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE5_RTJ_NAME=$(PHASE5_LOW_RTJ_NAME) ./hack/dev/inspect-priority.sh

phase5-inspect-policy:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE5_RTJ_NAME=$(PHASE5_LOW_RTJ_NAME) ./hack/dev/inspect-policy.sh

phase5-inspect-workload:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE5_RTJ_NAME=$(PHASE5_LOW_RTJ_NAME) ./hack/dev/inspect-workload-phase5.sh

phase5-inspect-checkpoints:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE5_RTJ_NAME=$(PHASE5_LOW_RTJ_NAME) ./hack/dev/inspect-checkpoints-phase5.sh

e2e-phase5:
	RUN_KIND_E2E=1 PHASE5_TRAINER_IMAGE=$(PHASE5_TRAINER_IMAGE) go test ./test/e2e -run 'TestProtectedPriority|TestPriorityDrop|TestYieldBudget' -v -timeout 20m

# ── Phase 6 targets ──────────────────────────────────────────────────
#
# phase6-up:          Create three kind clusters (manager + 2 workers),
#                     install Kueue, JobSet, MultiKueue, RTJ CRDs, and
#                     the shared checkpoint store. Full deterministic setup.
# phase6-down:        Delete all three Phase 6 kind clusters.
# phase6-status:      Show cluster state across all three clusters:
#                     MultiKueue resources, queues, RTJ CRDs, shared store.
# phase6-load-images: Load images into all three kind clusters.
# phase6-smoke:       Run Phase 6 infrastructure smoke test. Verifies
#                     cluster existence, Kueue, MultiKueue config,
#                     queues, RTJ CRDs, shared store, and dry-run RTJ.
#
# The Phase 6 profile exercises:
#   G1: MultiKueue external-framework integration for RTJ
#   G2: Manager/worker operator split (--mode flag)
#   G3: Shared-checkpoint remote pause/resume
#   G4: Manager-visible remote status
#   G5: Deterministic three-cluster local dev/test profile
#
# IMPORTANT: Phase 6 uses separate kind cluster names (phase6-manager,
# phase6-worker-1, phase6-worker-2) and does NOT interfere with the
# single-cluster dev path (checkpoint-phase1).

phase6-up:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  ./hack/dev/create-phase6-kind-clusters.sh
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  ./hack/dev/install-phase6-kueue.sh
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/install-phase6-multikueue.sh
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  ./hack/dev/install-phase6-operator.sh
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/install-phase6-shared-store.sh
	@echo
	@echo "Phase 6 multi-cluster dev environment is ready"
	@echo "  manager:  kind-$(PHASE6_MANAGER)"
	@echo "  worker-1: kind-$(PHASE6_WORKER_1)"
	@echo "  worker-2: kind-$(PHASE6_WORKER_2)"

phase6-down:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  ./hack/dev/delete-phase6-kind-clusters.sh

phase6-status:
	@echo "=== Phase 6 Multi-Cluster Status ==="
	@echo
	@echo "--- Manager: kind-$(PHASE6_MANAGER) ---"
	@kubectl cluster-info --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (not reachable)"
	@echo "nodes:"
	@kubectl get nodes --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || true
	@echo "kueue:"
	@kubectl -n kueue-system get deployment kueue-controller-manager --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || true
	@echo "multikueue:"
	@kubectl get admissionchecks.kueue.x-k8s.io --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (none)"
	@kubectl get multikueueconfigs.kueue.x-k8s.io --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (none)"
	@kubectl get multikueueclusters.kueue.x-k8s.io --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (none)"
	@echo "queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (none)"
	@echo "rtj crd:"
	@kubectl get crd resumabletrainingjobs.training.checkpoint.example.io --context "kind-$(PHASE6_MANAGER)" --no-headers 2>/dev/null || echo "  (not installed)"
	@echo
	@echo "--- Worker-1: kind-$(PHASE6_WORKER_1) ---"
	@kubectl get nodes --context "kind-$(PHASE6_WORKER_1)" 2>/dev/null || echo "  (not reachable)"
	@echo "kueue:"
	@kubectl -n kueue-system get deployment kueue-controller-manager --context "kind-$(PHASE6_WORKER_1)" 2>/dev/null || true
	@echo "queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io --context "kind-$(PHASE6_WORKER_1)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(PHASE6_WORKER_1)" 2>/dev/null || echo "  (none)"
	@echo "rtj crd:"
	@kubectl get crd resumabletrainingjobs.training.checkpoint.example.io --context "kind-$(PHASE6_WORKER_1)" --no-headers 2>/dev/null || echo "  (not installed)"
	@echo
	@echo "--- Worker-2: kind-$(PHASE6_WORKER_2) ---"
	@kubectl get nodes --context "kind-$(PHASE6_WORKER_2)" 2>/dev/null || echo "  (not reachable)"
	@echo "kueue:"
	@kubectl -n kueue-system get deployment kueue-controller-manager --context "kind-$(PHASE6_WORKER_2)" 2>/dev/null || true
	@echo "queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io --context "kind-$(PHASE6_WORKER_2)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(PHASE6_WORKER_2)" 2>/dev/null || echo "  (none)"
	@echo "rtj crd:"
	@kubectl get crd resumabletrainingjobs.training.checkpoint.example.io --context "kind-$(PHASE6_WORKER_2)" --no-headers 2>/dev/null || echo "  (not installed)"
	@echo
	@echo "--- Shared Checkpoint Store ---"
	@kubectl -n $(DEV_NAMESPACE) get configmap shared-checkpoint-store -o jsonpath='{.data}' --context "kind-$(PHASE6_MANAGER)" 2>/dev/null || echo "  (not configured)"
	@echo

phase6-load-images:
	@set -euo pipefail; \
	for cluster in $(PHASE6_MANAGER) $(PHASE6_WORKER_1) $(PHASE6_WORKER_2); do \
		for image in $(IMAGES); do \
			echo "loading $$image into kind cluster $$cluster"; \
			kind load docker-image --name "$$cluster" "$$image"; \
		done; \
	done

phase6-smoke:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-smoke.sh

# ── Phase 6 demo / inspect targets ─────────────────────────────────
#
# phase6-submit:              Submit a MultiKueue-managed RTJ on the manager.
# phase6-pause:               Pause the remote RTJ (manager patches desiredState).
# phase6-resume:              Resume the remote RTJ (manager patches desiredState).
# phase6-inspect-manager:     Inspect RTJ dispatch + MultiCluster status on manager.
# phase6-inspect-worker:      Inspect the mirror RTJ on worker clusters.
# phase6-inspect-checkpoints: Inspect shared checkpoint evidence across clusters.
# e2e-phase6:                 Run Phase 6 e2e tests (requires three kind clusters).

phase6-submit:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_RTJ_NAME=$(PHASE6_RTJ_NAME) \
	  PHASE6_TRAINER_IMAGE=$(PHASE6_TRAINER_IMAGE) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-submit-manager-rtj.sh

phase6-pause:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_RTJ_NAME=$(PHASE6_RTJ_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-pause-manager-rtj.sh

phase6-resume:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_RTJ_NAME=$(PHASE6_RTJ_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-resume-manager-rtj.sh

phase6-inspect-manager:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_RTJ_NAME=$(PHASE6_RTJ_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-inspect-manager.sh

phase6-inspect-worker:
	PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  PHASE6_RTJ_NAME=$(PHASE6_RTJ_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-inspect-worker.sh

phase6-inspect-checkpoints:
	PHASE6_MANAGER=$(PHASE6_MANAGER) \
	  PHASE6_WORKER_1=$(PHASE6_WORKER_1) \
	  PHASE6_WORKER_2=$(PHASE6_WORKER_2) \
	  PHASE6_RTJ_NAME=$(PHASE6_RTJ_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase6-inspect-checkpoints.sh

e2e-phase6:
	RUN_KIND_E2E=1 PHASE6_TRAINER_IMAGE=$(PHASE6_TRAINER_IMAGE) go test ./test/e2e -run 'TestMultiCluster' -v -timeout 30m

# ── Phase 7 targets ──────────────────────────────────────────────────
#
# phase7-up:                   Create kind cluster, install base stack, then
#                              apply Phase 7 capacity-guaranteed launch profile.
#                              Includes ProvisioningRequest CRD, fake backend,
#                              Kueue config with waitForPodsReady, and
#                              provisioning AdmissionCheck wiring.
# phase7-down:                 Delete the kind cluster.
# phase7-status:               Show cluster state including provisioning CRD,
#                              admission checks, queues, fake-provisioner,
#                              and waitForPodsReady config.
# phase7-load-images:          Load images into the Phase 7 cluster.
# phase7-smoke:                Run Phase 7 infrastructure smoke test. Verifies
#                              ProvisioningRequest API, fake backend, built-in
#                              AdmissionCheck, waitForPodsReady config, and
#                              sample RTJ dry-run.
# phase7-profile:              Apply/re-apply Phase 7 profile on existing cluster.
# phase7-build-fake-provisioner: Build the fake-provisioner Docker image.
#
# The Phase 7 profile exercises:
#   G1: ProvisioningRequest AdmissionCheck integration (Kueue built-in)
#   G2: Fake ProvisioningRequest backend (delayed success, failure, expiry)
#   G3: waitForPodsReady startup timeout and recovery
#   G4: Deterministic local dev/test profile
#
# Sample RTJs in deploy/dev/phase7/samples/:
#   rtj-delayed-success.yaml   — Successful delayed provisioning (10s)
#   rtj-provision-failure.yaml — Provisioning permanent failure
#   rtj-startup-timeout.yaml   — waitForPodsReady timeout path

PHASE7_RTJ_NAME ?= phase7-demo
PHASE7_TRAINER_IMAGE ?= $(PHASE6_TRAINER_IMAGE)
FAKE_PROVISIONER_IMAGE ?= fake-provisioner:dev
PHASE8_RTJ_NAME ?= phase8-demo
PHASE8_TRAINER_IMAGE ?= $(PHASE7_TRAINER_IMAGE)
PHASE8_KIND_NODE_IMAGE ?= kindest/node:v1.33.0
PHASE9_SHRINK_RTJ_NAME ?= phase9-elastic-a
PHASE9_GROW_RTJ_NAME ?= phase9-elastic-b
PHASE9_NONELASTIC_RTJ_NAME ?= phase9-fixed
PHASE9_TRAINER_IMAGE ?= $(PHASE8_TRAINER_IMAGE)

phase7-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/dev-up.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  FAKE_PROVISIONER_IMAGE=$(FAKE_PROVISIONER_IMAGE) \
	  ./hack/dev/install-phase7-profile.sh

phase7-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

phase7-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh
	@echo
	@echo "phase7 provisioning request CRD:"
	@kubectl get crd provisioningrequests.autoscaling.x-k8s.io --no-headers --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (not installed)"
	@echo
	@echo "phase7 admission checks:"
	@kubectl get admissionchecks.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase7-provisioning --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase7 provisioning request configs:"
	@kubectl get provisioningrequestconfigs.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase7-provisioning --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase7 queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io phase7-cq --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) phase7-training --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase7 fake-provisioner:"
	@kubectl -n $(DEV_NAMESPACE) get deployment fake-provisioner --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (not deployed)"
	@echo
	@echo "phase7 provisioning requests:"
	@kubectl get provisioningrequests.autoscaling.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"

phase7-load-images:
	@set -euo pipefail; \
	for image in $(IMAGES) $(FAKE_PROVISIONER_IMAGE); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

phase7-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase7-smoke.sh

phase7-profile:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  FAKE_PROVISIONER_IMAGE=$(FAKE_PROVISIONER_IMAGE) \
	  ./hack/dev/phase7-profile.sh

# ── Phase 7 demo / inspect targets ─────────────────────────────────
#
# phase7-submit-success:              Submit a delayed-success provisioning RTJ.
# phase7-submit-fail:                 Submit a provisioning failure RTJ.
# phase7-inspect-launchgate:          Inspect launch gate, provisioning state,
#                                     startup/recovery, and capacity guarantee.
# phase7-inspect-workload:            Inspect Workload admission, admission checks,
#                                     podSetUpdates, and child JobSet.
# phase7-inspect-provisioningrequest: Inspect ProvisioningRequest objects,
#                                     conditions, parameters, and fake-provisioner logs.
# phase7-inspect-checkpoints:         Inspect checkpoint evidence, startup/recovery
#                                     correlation, and child JobSet pod status.
# e2e-phase7:                         Run Phase 7 e2e tests.

phase7-submit-success:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE7_RTJ_NAME=$(PHASE7_RTJ_NAME) PHASE7_TRAINER_IMAGE=$(PHASE7_TRAINER_IMAGE) ./hack/dev/phase7-submit-success.sh

phase7-submit-fail:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE7_RTJ_NAME=phase7-fail PHASE7_TRAINER_IMAGE=$(PHASE7_TRAINER_IMAGE) ./hack/dev/phase7-submit-fail.sh

phase7-inspect-launchgate:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE7_RTJ_NAME=$(PHASE7_RTJ_NAME) ./hack/dev/phase7-inspect-launchgate.sh

phase7-inspect-workload:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE7_RTJ_NAME=$(PHASE7_RTJ_NAME) ./hack/dev/phase7-inspect-workload.sh

phase7-inspect-provisioningrequest:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE7_RTJ_NAME=$(PHASE7_RTJ_NAME) ./hack/dev/phase7-inspect-provisioningrequest.sh

phase7-inspect-checkpoints:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE7_RTJ_NAME=$(PHASE7_RTJ_NAME) ./hack/dev/phase7-inspect-checkpoints.sh

phase7-build-fake-provisioner:
	docker build -t $(FAKE_PROVISIONER_IMAGE) -f cmd/fake-provisioner/Dockerfile .

e2e-phase7:
	RUN_KIND_E2E=1 PHASE7_TRAINER_IMAGE=$(PHASE7_TRAINER_IMAGE) go test ./test/e2e -run 'TestCapacityGuaranteedLaunch|TestProvisioningFailureRequeue|TestWaitForPodsReadyTimeout' -v -timeout 20m

# ── Phase 8 targets ──────────────────────────────────────────────────
#
# phase8-up:          Create kind cluster with Kubernetes v1.33+ (DRA-capable),
#                     install base stack, then apply Phase 8 DRA profile.
#                     Installs: example DRA driver, DeviceClass, Kueue with
#                     deviceClassMappings, ClusterQueue with DRA device quota.
# phase8-down:        Delete the kind cluster.
# phase8-status:      Show cluster state including DRA driver, DeviceClass,
#                     ResourceSlices, queues, and deviceClassMappings.
# phase8-load-images: Load images into the Phase 8 cluster.
# phase8-smoke:       Run Phase 8 infrastructure smoke test. Verifies DRA APIs,
#                     example driver, DeviceClass, ResourceSlices, Kueue
#                     deviceClassMappings, queue health, and sample RTJ dry-run.
# phase8-profile:     Apply/re-apply Phase 8 profile on existing cluster.
#
# The Phase 8 profile exercises:
#   G1: Native DRA device requests on RTJ (DeviceClass + ResourceClaimTemplate)
#   G2: Kueue deviceClassMappings for DRA device quota accounting
#   G3: DRA-aware child JobSet rendering (claim injection)
#   G4: Device profile checkpoint compatibility (fail-closed)
#   G5: Deterministic local dev/test profile with simulated devices
#
# IMPORTANT: Phase 8 requires Kubernetes v1.33+ for stable DRA support.
# The kind cluster uses PHASE8_KIND_NODE_IMAGE (default: kindest/node:v1.33.0).
# This does NOT require real accelerators -- all devices are simulated.
#
# Sample RTJs in deploy/dev/phase8/samples/:
#   rtj-dra-launch.yaml            — Successful DRA-backed launch (2 GPUs)
#   rtj-dra-pause-resume.yaml      — Pause/resume with same device profile
#   rtj-dra-incompatible-profile.yaml — Incompatible device profile rejection

phase8-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  KIND_NODE_IMAGE=$(PHASE8_KIND_NODE_IMAGE) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/dev-up.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/install-phase8-profile.sh

phase8-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

phase8-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh
	@echo
	@echo "phase8 DRA driver:"
	@kubectl -n dra-example-driver get daemonset dra-example-driver --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (not deployed)"
	@echo
	@echo "phase8 DeviceClass:"
	@kubectl get deviceclasses.resource.k8s.io example-gpu --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (not found)"
	@echo
	@echo "phase8 ResourceSlices:"
	@kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase8 queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io phase8-cq --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) phase8-training --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase8 Kueue deviceClassMappings:"
	@kubectl -n kueue-system get configmap kueue-manager-config -o jsonpath='{.data.controller_manager_config\.yaml}' --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null | grep -A3 'deviceClassMappings' || echo "  (not configured)"
	@echo

phase8-load-images:
	@set -euo pipefail; \
	for image in $(IMAGES); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

phase8-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase8-smoke.sh

phase8-profile:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase8-profile.sh

# ── Phase 8 demo / inspect targets ─────────────────────────────────
#
# phase8-submit:              Submit a DRA-backed RTJ.
#                              PHASE8_SAMPLE=launch (default) | pause-resume | incompatible
# phase8-pause:               Pause the DRA-backed RTJ.
# phase8-resume:              Resume the DRA-backed RTJ.
# phase8-inspect-dra:         Inspect DRA device status: device mode, fingerprint,
#                              ResourceClaimTemplates, ResourceClaims, DeviceClass,
#                              ResourceSlices, allocation state.
# phase8-inspect-kueue:       Inspect Kueue accounting: ClusterQueue usage,
#                              Workload admission, deviceClassMappings resolution.
# phase8-inspect-checkpoints: Inspect checkpoint device-profile metadata:
#                              fingerprints, compatibility assessment, store contents.
# e2e-phase8:                 Run Phase 8 e2e tests.

phase8-submit:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE8_RTJ_NAME=$(PHASE8_RTJ_NAME) PHASE8_TRAINER_IMAGE=$(PHASE8_TRAINER_IMAGE) ./hack/dev/phase8-submit-dra.sh

phase8-pause:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE8_RTJ_NAME=$(PHASE8_RTJ_NAME) ./hack/dev/phase8-pause-dra.sh

phase8-resume:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE8_RTJ_NAME=$(PHASE8_RTJ_NAME) ./hack/dev/phase8-resume-dra.sh

phase8-inspect-dra:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE8_RTJ_NAME=$(PHASE8_RTJ_NAME) ./hack/dev/phase8-inspect-dra.sh

phase8-inspect-kueue:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE8_RTJ_NAME=$(PHASE8_RTJ_NAME) ./hack/dev/phase8-inspect-kueue.sh

phase8-inspect-checkpoints:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) PHASE8_RTJ_NAME=$(PHASE8_RTJ_NAME) ./hack/dev/phase8-inspect-checkpoints.sh

e2e-phase8:
	RUN_KIND_E2E=1 PHASE8_TRAINER_IMAGE=$(PHASE8_TRAINER_IMAGE) go test ./test/e2e -run 'TestDRAQuotaAndAllocation|TestDRAResumeCompatibleProfile|TestDRAIncompatibleResumeRejection' -v -timeout 20m

# ── Phase 9 targets ──────────────────────────────────────────────────
#
# phase9-up:          Create kind cluster, install base stack, then apply
#                     Phase 9 elastic resize profile. Includes queue with
#                     dynamic-reclaim quota sizing and RTJ external framework.
# phase9-down:        Delete the kind cluster.
# phase9-status:      Show cluster state including elastic queue profile,
#                     quota allocation, and reclaimablePods configuration.
# phase9-load-images: Load images into the Phase 9 cluster.
# phase9-smoke:       Run Phase 9 infrastructure smoke test. Verifies:
#                       - RTJ CRDs with elasticity fields
#                       - Phase 9 Kueue profile is active
#                       - Sample elastic RTJs validate (dry-run)
#                       - Runtime fixture knobs are present in manifests
#                       - reclaimablePods patching path is configured
# phase9-profile:     Apply/re-apply Phase 9 profile on existing cluster.
#
# The Phase 9 profile exercises:
#   G1: Manual target-based elastic resize (shrink and grow)
#   G2: In-place shrink via reclaimablePods SSA patching
#   G3: Grow via checkpoint-and-relaunch
#   G4: Dynamic quota reclaim (RTJ A shrinks → RTJ B admitted)
#   G5: Non-elastic fallback (Phase 8 behavior preserved)
#   G6: Deterministic local dev/test profile
#
# Queue quota design:
#   1250m CPU / 1280Mi memory — enough for one 4-worker RTJ but not two.
#   After RTJ A shrinks 4→2, released quota admits RTJ B (2 workers).
#
# Sample RTJs in deploy/dev/phase9/samples/:
#   rtj-elastic-shrink.yaml   — 4 workers, shrink to 2 via reclaimablePods
#   rtj-elastic-grow.yaml     — 2 workers, grow to 4 via relaunch
#   rtj-non-elastic.yaml      — 2 workers, no elasticity (backward compat)

phase9-up:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/dev-up.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/install-phase9-profile.sh

phase9-down:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/dev-down.sh

phase9-status:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/status.sh
	@echo
	@echo "phase9 queues:"
	@kubectl get clusterqueues.kueue.x-k8s.io phase9-cq --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@kubectl get localqueues.kueue.x-k8s.io -n $(DEV_NAMESPACE) phase9-training --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase9 quota (dynamic reclaim):"
	@kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o jsonpath='{.spec.resourceGroups[0].flavors[0].resources}' --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (not found)"
	@echo
	@echo
	@echo "phase9 workloads:"
	@kubectl get workloads.kueue.x-k8s.io -n $(DEV_NAMESPACE) --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo
	@echo "phase9 RTJs:"
	@kubectl get resumabletrainingjobs.training.checkpoint.example.io -n $(DEV_NAMESPACE) --context "kind-$(KIND_CLUSTER_NAME)" 2>/dev/null || echo "  (none)"
	@echo

phase9-load-images:
	@set -euo pipefail; \
	for image in $(IMAGES); do \
		echo "loading $$image into kind cluster $(KIND_CLUSTER_NAME)"; \
		kind load docker-image --name $(KIND_CLUSTER_NAME) "$$image"; \
	done

phase9-smoke:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DEV_NAMESPACE=$(DEV_NAMESPACE) ./hack/dev/phase9-smoke.sh

phase9-profile:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
	  DEV_NAMESPACE=$(DEV_NAMESPACE) \
	  ./hack/dev/phase9-profile.sh
