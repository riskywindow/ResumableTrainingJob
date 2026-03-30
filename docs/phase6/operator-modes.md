# Phase 6: Operator Modes

## Overview

Phase 6 introduces a startup-time operator mode that splits RTJ controller
behavior into manager and worker roles. The mode is set via the `--mode`
flag and is fixed for the process lifetime. There is no runtime mode
switching.

## Modes

### Worker (default)

```
--mode=worker
```

The operator runs the full Phase 5 runtime path:

- Launch gating (Phase 4)
- Child JobSet creation and lifecycle management
- Control ConfigMap creation
- Checkpoint I/O (catalog queries, manifest validation)
- Graceful yield and drain
- Resume from checkpoint
- Priority shaping (Phase 5)

Worker mode is used for:
- **Single-cluster deployments** (no MultiKueue)
- **Worker clusters in a MultiKueue setup**

Worker mode treats all RTJs as runtime-bearing objects. The `spec.managedBy`
field is ignored by the worker-mode reconciler — even MultiKueue-managed
RTJs get the full runtime path on the worker cluster, because the worker
receives a mirrored copy from MultiKueue that it manages locally.

### Manager

```
--mode=manager
```

The operator runs in control-plane-only mode. Behavior depends on whether
each individual RTJ is managed by MultiKueue:

**For MultiKueue-managed RTJs** (`spec.managedBy == "kueue.x-k8s.io/multikueue"`):
- Local child JobSet creation is **suppressed**
- Control ConfigMap creation is **suppressed**
- No checkpoint I/O (no S3 access needed)
- No launch gate evaluation
- No resume/launch path execution
- `status.multiCluster.localExecutionSuppressed` is set to `true`
- `status.multiCluster.dispatchPhase` is initialized to `Pending`
- Phase is set to `Queued` with reason `LocalExecutionSuppressed`

**For non-MultiKueue RTJs** (no `spec.managedBy` or different value):
- Full Phase 5 runtime path is preserved
- This handles the edge case where a non-MultiKueue RTJ exists on a
  manager cluster and prevents data loss

## Mode Detection

Mode detection is intentionally simple:

```
ShouldSuppressRuntime = (mode == "manager") AND (job.IsManagedByMultiKueue())
```

This is an all-or-nothing startup flag — not per-RTJ. The per-RTJ behavior
difference comes from the `spec.managedBy` field on each RTJ.

## Ownership Split

| Responsibility | Manager (MultiKueue RTJ) | Worker | Single-Cluster |
|---|---|---|---|
| Finalizer management | Yes | Yes | Yes |
| Status initialization | Yes | Yes | Yes |
| Child JobSet lifecycle | **No** | Yes | Yes |
| Control ConfigMap | **No** | Yes | Yes |
| Launch gate evaluation | **No** | Yes | Yes |
| Checkpoint I/O | **No** | Yes | Yes |
| Graceful yield/drain | **No** | Yes | Yes |
| Resume from checkpoint | **No** | Yes | Yes |
| Priority shaping | **No** | Yes | Yes |
| MultiCluster status | Yes | No | No |
| Desired state intent | Yes | Yes | Yes |

## Backward Compatibility

- **Zero-value mode** (empty string): Behaves identically to `ModeWorker`.
  `ShouldSuppressRuntime` returns `false` for any non-manager mode,
  preserving pre-Phase 6 behavior.

- **Existing deployments**: No flag change required. The default `--mode=worker`
  produces identical behavior to the pre-Phase 6 controller.

- **Existing tests**: All Phase 1-5 tests pass without modification because
  they use a zero-value or unset Mode field, which triggers no suppression.

## Configuration

### Single-Cluster Deployment (unchanged)

```yaml
# No --mode flag needed, defaults to worker
containers:
  - name: operator
    args:
      - --metrics-bind-address=:8080
```

### Multi-Cluster: Manager Cluster

```yaml
containers:
  - name: operator
    args:
      - --mode=manager
      - --metrics-bind-address=:8080
```

### Multi-Cluster: Worker Cluster

```yaml
containers:
  - name: operator
    args:
      - --mode=worker
      - --metrics-bind-address=:8080
```

## Test Coverage

The following scenarios are tested in `internal/controller/mode_test.go`:

1. **ParseOperatorMode** — valid and invalid mode strings
2. **ShouldSuppressRuntime** — all four combinations of (mode, managedBy)
3. **Manager mode suppresses runtime** — MultiKueue-managed RTJ gets no
   child JobSet, no ConfigMap, MultiCluster status is populated
4. **Manager mode allows normal path** — non-MultiKueue RTJ on manager
   cluster gets the full runtime path
5. **Worker mode launches runtime** — MultiKueue-managed RTJ on worker
   gets full runtime
6. **Single-cluster unchanged** — default mode, no managedBy, full path
7. **No accidental JobSet creation** — admitted (unsuspended) MultiKueue
   RTJ in manager mode still gets no child JobSet
8. **Idempotent reconciliation** — repeated reconcile converges without
   creating runtime resources
9. **Zero-value mode** — empty Mode field preserves pre-Phase 6 behavior

## Design Decisions

1. **Two modes, not three.** Worker covers both single-cluster and
   multi-cluster worker roles. A separate "single" mode adds complexity
   without behavioral difference.

2. **Mode is startup-time, not per-RTJ.** The `--mode` flag sets the
   process-wide behavior. Per-RTJ routing comes from `spec.managedBy`.
   This keeps the model simple and explicit.

3. **Manager preserves normal path for non-MultiKueue RTJs.** Safety-first:
   if a non-MultiKueue RTJ exists on a manager cluster, we don't silently
   drop it. The full Phase 5 path runs for those RTJs.

4. **Suppression check is early in Reconcile.** The `ShouldSuppressRuntime`
   check occurs after finalizer and status initialization but before any
   runtime path code (getActiveJobSet, launch gates, launch/resume). This
   ensures no runtime resources are accidentally created.

5. **No MultiKueue dispatch wiring yet.** This session implements the mode
   split only. MultiKueue configuration, external-framework protocol, and
   status mirroring are deferred to subsequent sessions.
