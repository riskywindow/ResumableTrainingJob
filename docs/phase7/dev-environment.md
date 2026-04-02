# Phase 7 — Local Dev Environment

## Overview

The Phase 7 dev profile creates a deterministic local environment for testing
the capacity-guaranteed launch pipeline. It provides:

1. **ProvisioningRequest CRD** — Minimal dev-only CRD (`autoscaling.x-k8s.io/v1beta1`)
   that matches what Kueue v0.15.1's built-in ProvisioningRequest AdmissionCheck
   controller expects. In production this CRD comes from the cluster-autoscaler.

2. **Fake ProvisioningRequest backend** — A small controller (`cmd/fake-provisioner`)
   that watches ProvisioningRequest objects and updates their status conditions
   deterministically. No real cluster-autoscaler is needed.

3. **Kueue config with waitForPodsReady** — Enables startup timeout detection and
   automatic requeuing with backoff.

4. **ProvisioningRequest AdmissionCheck wiring** — Uses Kueue's built-in
   `kueue.x-k8s.io/provisioning` controller with ProvisioningRequestConfig
   objects that reference the fake backend's provisioning classes.

## Quick Start

```bash
# Full environment from scratch (kind + base stack + Phase 7 profile)
make phase7-up

# Validate everything is wired correctly
make phase7-smoke

# Show status
make phase7-status

# Tear down
make phase7-down
```

### On an existing cluster

```bash
# Apply Phase 7 profile to a running dev cluster
make phase7-profile

# Rebuild and reload the fake-provisioner image
make phase7-build-fake-provisioner
make phase7-load-images
```

## Fake Provisioner Backend

The fake provisioner (`cmd/fake-provisioner/main.go`) is a controller-runtime
manager that watches `ProvisioningRequest` objects and updates their status
conditions based on the `spec.provisioningClassName`.

### Supported Classes

| Class Name | Behavior | Parameters |
|---|---|---|
| `check-capacity.fake.dev` | Delayed success (default 10s) | `fake.dev/delay` |
| `failed.fake.dev` | Immediate permanent failure | `fake.dev/failure-message` |
| `booking-expiry.fake.dev` | Success then revocation | `fake.dev/delay`, `fake.dev/expiry` |

### How It Works

1. Kueue's built-in ProvisioningRequest AC controller creates a
   `ProvisioningRequest` object when a Workload needs provisioning.
2. The fake backend sees the PR and reads `spec.provisioningClassName`.
3. Based on the class, it deterministically updates `status.conditions`:
   - `Provisioned=True` — capacity is available
   - `Failed=True` — provisioning permanently failed
   - `CapacityRevoked=True` — previously available capacity revoked
4. Kueue watches the PR conditions and maps them to AdmissionCheck state
   on the Workload.

### Parameters

Parameters are set on the `ProvisioningRequestConfig` and flow through to the
`ProvisioningRequest.spec.parameters`. The fake backend reads them:

- **`fake.dev/delay`** — Duration before success (default `10s`)
- **`fake.dev/expiry`** — Duration after success before revocation (default `60s`)
- **`fake.dev/failure-message`** — Custom failure message string

## Kueue Configuration

### waitForPodsReady

The Phase 7 Kueue config enables `waitForPodsReady` which monitors whether
pods for admitted workloads actually become ready within a timeout:

```yaml
waitForPodsReady:
  enable: true
  timeout: 120s
  requeuingStrategy:
    timestamp: Eviction
    backoffLimitCount: 3
    backoffBaseSeconds: 10
    backoffMaxSeconds: 300
```

When pods fail to start within 120s, Kueue evicts the Workload and requeues
it with exponential backoff (10s base, 300s max, 3 retries).

### Feature Gates

- **`ProvisioningACC: true`** — Ensures the built-in Provisioning AdmissionCheck
  Controller is active.

## Queue Profile

```
ClusterQueue: phase7-cq
  ├── AdmissionCheck: dev-provisioning (kueue.x-k8s.io/provisioning)
  │     └── ProvisioningRequestConfig: dev-provisioning-config
  │           └── provisioningClassName: check-capacity.fake.dev
  ├── ResourceGroup: cpu (500m), memory (512Mi)
  │     └── Flavor: default-flavor
  └── Preemption: withinClusterQueue=LowerPriority, reclaimWithinCohort=Never

LocalQueue: phase7-training (namespace: checkpoint-dev)
  └── clusterQueue: phase7-cq
```

Additional queues for testing:
- `phase7-failure-cq` / `phase7-failure-training` — uses `dev-provisioning-failure`
  AdmissionCheck (permanent failure class)

## Sample RTJs

| File | Description | Expected Outcome |
|---|---|---|
| `rtj-delayed-success.yaml` | Normal delayed provisioning | Admitted after ~10s delay |
| `rtj-provision-failure.yaml` | Provisioning failure path | Rejected, stays suspended |
| `rtj-startup-timeout.yaml` | Nonexistent image, triggers waitForPodsReady | Evicted after 120s, requeued |

## File Layout

```
cmd/fake-provisioner/
  main.go                          Entry point for fake-provisioner binary
  Dockerfile                       Docker build for the fake-provisioner

internal/fakeprovisioner/
  controller.go                    Reconciler + pure action computation
  controller_test.go               Unit tests for action computation
  status.go                        Condition helpers for unstructured objects
  status_test.go                   Unit tests for condition helpers

deploy/dev/phase7/
  kueue/
    controller_manager_config.phase7.yaml   Kueue config (waitForPodsReady + ProvisioningACC)
  provisioning/
    00-provisioning-request-crd.yaml        Dev-only ProvisioningRequest CRD
    10-provisioning-request-config.yaml     ProvisioningRequestConfig objects
    20-admission-check.yaml                 AdmissionCheck objects
  queues/
    10-cluster-queue.yaml                   ClusterQueue with admission check
    20-local-queue.yaml                     LocalQueue
  fake-provisioner/
    00-service-account.yaml                 ServiceAccount
    10-rbac.yaml                            ClusterRole + ClusterRoleBinding
    20-deployment.yaml                      Deployment
  samples/
    rtj-delayed-success.yaml                Delayed success sample
    rtj-provision-failure.yaml              Provisioning failure sample
    rtj-startup-timeout.yaml                Startup timeout sample
    failure-queue.yaml                      Auxiliary queue for failure testing

hack/dev/
  install-phase7-profile.sh        Full Phase 7 profile installer
  phase7-profile.sh                Re-apply wrapper
  phase7-smoke.sh                  Infrastructure smoke test
```

## Design Decisions

1. **Unstructured client for fake provisioner** — The fake backend uses
   controller-runtime with `unstructured.Unstructured` objects to avoid
   importing the cluster-autoscaler ProvisioningRequest Go types. This
   keeps the dependency tree minimal.

2. **provisioningClassName convention** — Behavior is controlled via the
   provisioning class name, not a separate configuration CRD. This is the
   smallest practical mechanism.

3. **Pure action computation** — The core reconciliation logic (`ComputeAction`)
   is a pure function of (class, conditions, timestamps, params, now). This
   makes it trivially testable without a Kubernetes client.

4. **Delay via creation timestamp** — The fake backend computes readiness by
   comparing `creationTimestamp + delay` to `now`. No in-memory state or
   timers are needed.

5. **Separate binary** — The fake provisioner is a standalone binary
   (`cmd/fake-provisioner`) rather than embedded in the RTJ operator.
   This keeps the dev tooling cleanly separated from production code.
