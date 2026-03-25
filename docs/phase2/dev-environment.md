# Phase 2 Dev Environment

## Goal

Phase 2 keeps the existing local `kind` stack from Phase 1 and tightens it so native RTJ admission can be exercised safely.

The default local profile is correctness-first:

- pinned `kind`, Kueue, JobSet, and MinIO versions
- RTJ registered as a Kueue external framework
- one opt-in development namespace for Kueue-managed admission
- one priority-preemption queue sized for exactly one RTJ at a time
- unlabeled runtime child `JobSet` objects left outside Kueue management

## Reused Base Stack

The environment intentionally reuses the Phase 1 stack unless a blocker exists:

- `kindest/node:v1.31.2`
- Kueue `v0.15.1`
- JobSet `v0.10.1`
- the same `checkpoint-dev` namespace
- the same MinIO development object store

This keeps Phase 2 local setup aligned with the already-working CPU-first path.

## Kueue Manager Configuration

The patched local Kueue manager config lives in [controller_manager_config.phase2-rtj-external-framework.yaml](/Users/rishivinodkumar/Daedelus/deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml).

The important changes are:

- `integrations.externalFrameworks` includes `ResumableTrainingJob.v1alpha1.training.checkpoint.example.io`
- `manageJobsWithoutQueueName: false`
- `managedJobsNamespaceSelector.matchLabels.checkpoint-native.dev/kueue-managed = "true"`

This combination is deliberate:

1. RTJ is still recognized by Kueue because it is an explicitly managed external framework and carries queue identity on the RTJ object.
2. The child `JobSet` is not accidentally managed because it is unlabeled and `manageJobsWithoutQueueName` is explicitly disabled.
3. The namespace selector keeps the local environment aligned with an opt-in namespace model for any future pod-style integrations, but the actual child-`JobSet` safety boundary in Phase 2 is the explicit `manageJobsWithoutQueueName: false` setting plus removing queue identity from the child object.

The local namespace label is applied by [00-checkpoint-dev.yaml](/Users/rishivinodkumar/Daedelus/deploy/dev/namespaces/00-checkpoint-dev.yaml).

## Queue Profile

The primary local queue profile is a single `ClusterQueue` plus one `LocalQueue`:

- `ResourceFlavor`: `default-flavor`
- `ClusterQueue`: `checkpoint-dev-cq`
- `LocalQueue`: `training`

The queue is configured in [10-cluster-queue.yaml](/Users/rishivinodkumar/Daedelus/deploy/dev/queues/10-cluster-queue.yaml) with:

- `cpu: 1`
- `memory: 1Gi`
- `preemption.withinClusterQueue: LowerPriority`

That shape is intentional:

- one RTJ from the current example template fits at a time
- a second lower-priority workload stays pending
- a higher-priority RTJ can preempt a lower-priority admitted RTJ inside the same queue

The local workload-priority classes are:

- `phase1-dev`
- `phase1-high`

The names are retained for compatibility with existing example manifests, but the semantics are now the Phase 2 local preemption profile.

## Scripts And Targets

The main entrypoints are:

- `make dev-up`
- `make dev-down`
- `make dev-status`
- `make load-images IMAGES=...`
- `make dev-smoke`
- `make phase2-smoke`

The scripts behind them are:

- [dev-up.sh](/Users/rishivinodkumar/Daedelus/hack/dev/dev-up.sh)
- [dev-down.sh](/Users/rishivinodkumar/Daedelus/hack/dev/dev-down.sh)
- [install-kueue.sh](/Users/rishivinodkumar/Daedelus/hack/dev/install-kueue.sh)
- [patch-kueue-config.sh](/Users/rishivinodkumar/Daedelus/hack/dev/patch-kueue-config.sh)
- [phase2-smoke.sh](/Users/rishivinodkumar/Daedelus/hack/dev/phase2-smoke.sh)

`phase2-smoke` is intentionally not a full e2e test.
It validates that:

- the Kueue manager config was patched correctly
- the namespace opt-in label and selector are present
- queue-labeled workloads are still admitted
- the local stack remains usable before RTJ-specific live tests are added

## Safe Local Workflow

1. Run `make dev-up`.
2. Start the operator separately.
3. Build and load the trainer image.
4. Run `make phase2-smoke` to verify the local queueing surface.
5. Submit RTJs that use `spec.queueName=training` and either `phase1-dev` or `phase1-high`.

## Fair Sharing

A secondary fair-sharing profile is not enabled by default in this pass.
The priority-preemption path is the authoritative local Phase 2 profile because it is narrower and more deterministic for suspend and preemption testing.
