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

.PHONY: dev-up dev-down dev-status dev-smoke phase2-smoke load-images submit-example pause-example resume-example inspect-example submit-low submit-high inspect-rtj inspect-kueue e2e e2e-phase2
.PHONY: phase3-up phase3-down phase3-status phase3-load-images phase3-smoke phase3-profile e2e-phase3
.PHONY: phase3-submit-flavor phase3-submit-flex phase3-inspect-admission phase3-inspect-checkpoints

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
