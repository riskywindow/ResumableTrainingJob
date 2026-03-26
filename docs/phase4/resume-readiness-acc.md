# ResumeReadiness AdmissionCheck Controller

Phase 4 — Goal G3

## Overview

The ResumeReadiness AdmissionCheck controller is a custom Kueue admission
check that gates workload admission until resume-readiness conditions are
met. It integrates with the existing Kueue admission pipeline by:

1. Maintaining the `Active` condition on AdmissionCheck objects that use its
   controller name.
2. Updating `AdmissionCheckState` entries on Workload objects to signal
   readiness (or rejection) to Kueue.

## Controller Name

```
training.checkpoint.example.io/resume-readiness
```

Defined in `internal/admissionchecks/resume/constants.go`.

## Architecture

The controller consists of two reconcilers running in the main operator
binary (no second binary needed):

### AdmissionCheck Reconciler

- **Watches:** `kueue.x-k8s.io/v1beta2.AdmissionCheck`
- **Filters:** Only processes checks where `spec.controllerName` matches
  `training.checkpoint.example.io/resume-readiness`.
- **Behavior:** Sets the `Active` condition to `True` when:
  - The `spec.parameters` reference exists and points to a
    `ResumeReadinessPolicy` in the correct API group.
  - The referenced `ResumeReadinessPolicy` object exists.
- **Behavior:** Sets `Active` to `False` with appropriate reason when
  the policy is missing or the parameters reference is invalid.

### Workload Reconciler

- **Watches:** `kueue.x-k8s.io/v1beta2.Workload`
- **Filters:** Only processes workloads that have an `AdmissionCheckState`
  entry for a check managed by this controller.
- **Behavior (scaffold):** Unconditionally sets managed checks to `Ready`.
  The actual readiness decision logic is deferred to a future session.

## ResumeReadinessPolicy CRD

A cluster-scoped parameter object referenced by AdmissionCheck resources.

```yaml
apiVersion: training.checkpoint.example.io/v1alpha1
kind: ResumeReadinessPolicy
metadata:
  name: default-resume-readiness
spec:
  requireCompleteCheckpoint: true        # Default: true
  allowInitialLaunchWithoutCheckpoint: true  # Default: true
  failurePolicy: FailClosed             # Default: FailClosed
  # maxCheckpointAge: 1h                # Default: not set (no limit)
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `requireCompleteCheckpoint` | `*bool` | `true` | Whether resume requires a complete checkpoint |
| `maxCheckpointAge` | `*metav1.Duration` | not set | Maximum acceptable checkpoint age (0 = no limit) |
| `failurePolicy` | `FailurePolicy` | `FailClosed` | Behavior when store/catalog is unreachable |
| `allowInitialLaunchWithoutCheckpoint` | `*bool` | `true` | Whether first launch is allowed without checkpoint |

## Sample AdmissionCheck

```yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: AdmissionCheck
metadata:
  name: resume-readiness
spec:
  controllerName: training.checkpoint.example.io/resume-readiness
  parameters:
    apiGroup: training.checkpoint.example.io
    kind: ResumeReadinessPolicy
    name: default-resume-readiness
```

## Sample ClusterQueue with Check

```yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: ClusterQueue
metadata:
  name: training-cq
spec:
  admissionChecksStrategy:
    admissionChecks:
      - name: resume-readiness
  # ... resourceGroups, preemption, etc.
```

## Wiring

The controller is wired into the main operator binary via
`internal/admissionchecks/resume/setup.go`, called from
`cmd/operator/main.go`. No second binary is needed because:

- The controller is lightweight (two reconcilers, no heavy dependencies).
- It shares the same scheme, leader election, and health checks.
- Deployment complexity is lower with a single binary.

## File Index

| File | Purpose |
|------|---------|
| `api/v1alpha1/resumereadinesspolicy_types.go` | CRD types and defaults |
| `api/v1alpha1/resumereadinesspolicy_webhook.go` | Webhook handlers |
| `api/v1alpha1/resumereadinesspolicy_webhook_test.go` | Webhook tests |
| `api/v1alpha1/zz_generated.deepcopy.go` | DeepCopy methods |
| `internal/admissionchecks/resume/constants.go` | Controller name and condition constants |
| `internal/admissionchecks/resume/admissioncheck_reconciler.go` | AdmissionCheck Active condition reconciler |
| `internal/admissionchecks/resume/workload_reconciler.go` | Workload AdmissionCheckState reconciler |
| `internal/admissionchecks/resume/setup.go` | Manager wiring |
| `internal/admissionchecks/resume/setup_test.go` | Registration and reconciler tests |
| `config/crd/bases/training.checkpoint.example.io_resumereadinesspolicies.yaml` | CRD manifest |
| `config/rbac/role.yaml` | RBAC for AdmissionCheck and policy access |
| `deploy/dev/admissionchecks/` | Sample manifests |

## Scaffold Status

This is a **scaffold only**. The readiness decision logic is not yet
implemented. The Workload reconciler unconditionally marks checks as
`Ready`. The following must be implemented in a future session:

- [ ] Checkpoint completeness verification
- [ ] Checkpoint age verification
- [ ] Store/catalog failure handling (FailOpen/FailClosed)
- [ ] Initial launch detection
- [ ] Integration with the RTJ checkpoint catalog
- [ ] Topology assignment validation (G4 integration)
