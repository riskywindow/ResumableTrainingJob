# Phase 5 Troubleshooting

Common issues with checkpoint-aware priority shaping and their solutions.

---

## No effective priority updates appearing

**Symptom:** The RTJ's `status.priorityShaping` is nil or not being updated.
The `PriorityShaping` condition is absent. The Workload's `spec.priority`
does not change.

**Possible causes:**

1. **No policy attached.** Check that the RTJ has `spec.priorityPolicyRef`:

   ```bash
   kubectl -n checkpoint-dev get rtj <name> \
     -o jsonpath='{.spec.priorityPolicyRef.name}'
   ```

   If empty, priority shaping is disabled (Phase 4 behavior). Add a
   `priorityPolicyRef` to the RTJ spec.

2. **RTJ is not in an active phase.** Priority shaping only evaluates in
   phases: Starting, Running, Restoring, YieldRequested, Draining. Queued
   RTJs reset to base priority.

   ```bash
   kubectl -n checkpoint-dev get rtj <name> -o jsonpath='{.status.phase}'
   ```

3. **CheckpointPriorityPolicy not found.** The policy referenced by the RTJ
   does not exist:

   ```bash
   kubectl get checkpointprioritypolicies.training.checkpoint.example.io
   ```

   Check the `PriorityShaping` condition for `PolicyResolutionFailed`.

4. **WorkloadPriorityClass not found.** The RTJ's
   `spec.workloadPriorityClassName` references a non-existent class:

   ```bash
   kubectl get workloadpriorityclasses.kueue.x-k8s.io
   ```

   Check the `PriorityShaping` condition for `BasePriorityResolutionFailed`.

5. **Operator not running Phase 5 code.** Verify the operator startup log
   includes `phase5Metrics=true`.

**Resolution:** Fix the root cause from the list above. Once the policy is
resolvable and the RTJ enters an active phase, priority shaping will
evaluate on the next reconcile.

---

## Workload priority being clobbered

**Symptom:** The Workload's `spec.priority` keeps reverting to the base
priority or a different value, overriding the effective priority set by the
operator.

**Possible causes:**

1. **Multiple controllers writing to the same Workload.** Verify only one
   operator instance is running:

   ```bash
   kubectl get pods -A -l app=checkpoint-native-operator
   ```

2. **Kueue GenericJob reconciler interference.** In Kueue v0.15.1, the
   GenericJob reconciler sets `Spec.Priority` only at Workload creation time
   (from the WorkloadPriorityClass). Subsequent reconciles do NOT overwrite
   the field. If you see clobbering, check your Kueue version:

   ```bash
   kubectl -n kueue-system get deployment kueue-controller-manager \
     -o jsonpath='{.spec.template.spec.containers[0].image}'
   ```

   Phase 5 is tested against Kueue v0.15.1. Other versions may behave
   differently.

3. **RTJ reconcile loop resetting priority.** When the RTJ transitions to
   Queued phase, `clearPriorityShapingOnQueued()` resets the priority shaping
   status. This is expected — queued RTJs should use base priority for
   re-admission ordering.

**Resolution:** Ensure Kueue v0.15.1 and a single operator instance. If the
issue persists, check operator logs for `patched Workload effective priority`
messages and compare timestamps with the Workload's `metadata.resourceVersion`.

---

## RTJ staying protected forever

**Symptom:** The RTJ remains in `preemptionState: Protected` and the
protection window never expires.

**Possible causes:**

1. **Protection window is very long.** Check the policy:

   ```bash
   kubectl get cpp <policy-name> \
     -o jsonpath='startupProtectionWindow={.spec.startupProtectionWindow}'
   ```

   The dev profile uses 30s. Production policies may use longer windows.

2. **Requeue not firing.** The operator sets `RequeueAfter` to
   `protectedUntil + 1s` when the RTJ is in Protected state. If the
   operator is restarting frequently or the reconcile loop is blocked, the
   requeue may not fire.

   Check operator logs:

   ```bash
   kubectl logs <operator-pod> | grep -i "requeue\|priority"
   ```

3. **Clock skew.** The protection window anchor uses `max(RunStartTime,
   LastResumeTime)`. If the cluster's clock is significantly skewed, the
   computed `protectedUntil` may be in the far future.

   ```bash
   kubectl -n checkpoint-dev get rtj <name> \
     -o jsonpath='protectedUntil={.status.priorityShaping.protectedUntil}'
   ```

4. **Repeated resume cycles.** The protection window resets on every resume.
   If the RTJ is being repeatedly preempted and resumed, it may appear to
   stay protected because each resume starts a fresh protection window.

   Check the yield history:

   ```bash
   kubectl -n checkpoint-dev get rtj <name> \
     -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/yield-history}'
   ```

**Resolution:** For the dev profile, wait at least 35s after the last start
or resume. If the protection window is excessively long in a custom policy,
reduce `startupProtectionWindow`. If the RTJ is cycling through resume, check
why it keeps getting preempted (likely a resource contention issue).

---

## RTJ becoming preemptible too early

**Symptom:** The RTJ transitions to `preemptionState: Preemptible` before
the checkpoint has had a reasonable chance to become stale.

**Possible causes:**

1. **Checkpoint freshness target too short.** Check the policy:

   ```bash
   kubectl get cpp <policy-name> \
     -o jsonpath='checkpointFreshnessTarget={.spec.checkpointFreshnessTarget}'
   ```

   The dev profile uses 60s. If the trainer checkpoints every 15s, this
   leaves a 45s margin before staleness. If checkpoints are slower, increase
   the freshness target.

2. **No checkpoint telemetry available (fail-closed).** If the checkpoint
   store is unreachable and `failOpenOnCheckpointStoreErrors` is false, the
   RTJ will be treated as having a stale checkpoint:

   ```bash
   kubectl get cpp <policy-name> \
     -o jsonpath=$'failOpenOnCheckpointStoreErrors={.spec.failOpenOnCheckpointStoreErrors}\nfailOpenOnTelemetryLoss={.spec.failOpenOnTelemetryLoss}'
   ```

   Check the preemption state reason:

   ```bash
   kubectl -n checkpoint-dev get rtj <name> \
     -o jsonpath='{.status.priorityShaping.preemptionStateReason}'
   ```

   Reasons `StoreErrorFailClosed` or `TelemetryUnavailableFailClosed` confirm
   this is a telemetry issue, not a genuine staleness issue.

3. **Protection window expired but checkpoint hasn't arrived yet.** If the
   protection window (30s in dev) expires before the first checkpoint lands
   (~15s in dev), and the freshness target (60s) is measured from job start,
   the RTJ goes through Active (no checkpoint yet = TelemetryUnknown
   fail-open) before potentially transitioning.

   Verify checkpoint timing:

   ```bash
   make phase5-inspect-checkpoints PHASE5_RTJ_NAME=<name>
   ```

4. **Trainer not checkpointing.** Verify the trainer is actually producing
   checkpoints:

   ```bash
   # Check MinIO for checkpoint manifests
   kubectl -n checkpoint-dev port-forward svc/minio 9000:9000 &
   curl -s http://localhost:9000/rtj-checkpoints/ | python3 -m json.tool
   ```

**Resolution:** For telemetry issues, set `failOpenOnTelemetryLoss: true`
and fix the checkpoint store connectivity. For timing issues, increase
`checkpointFreshnessTarget` or `startupProtectionWindow`. For trainer
issues, check the trainer logs and env vars (CHECKPOINT_EVERY, SLEEP_PER_STEP).

---

## E2E timing/config issues with checkpoint cadence

**Symptom:** Phase 5 e2e tests fail with timeouts or unexpected state
transitions. The lifecycle test does not see the expected state machine
progression.

**Possible causes:**

1. **Trainer image not loaded.** The trainer image must be pre-loaded into
   the kind cluster:

   ```bash
   kind load docker-image --name checkpoint-phase1 $PHASE5_TRAINER_IMAGE
   ```

2. **Phase 5 profile not installed.** The e2e tests expect the Phase 5
   infrastructure (priority classes, queues, policies):

   ```bash
   make phase5-smoke
   ```

   If the smoke test fails, re-install the profile:

   ```bash
   make phase5-profile
   ```

3. **Checkpoint timing mismatch.** The e2e tests use specific trainer
   settings (SLEEP_PER_STEP, CHECKPOINT_EVERY) to create deterministic
   timing. If the trainer image uses different defaults, the state machine
   may not progress as expected.

   Check the RTJ sample manifests in `deploy/dev/phase5/samples/` for the
   expected env vars.

4. **Cluster resource pressure.** The Phase 5 queue has tight quotas
   (500m CPU / 512Mi). If the cluster is under resource pressure, pods
   may not schedule promptly, causing timeouts.

   ```bash
   kubectl top nodes --context kind-checkpoint-phase1
   kubectl -n checkpoint-dev get pods -o wide
   ```

5. **Policy freshness target vs test timeouts.** The lifecycle test
   (`TestPriorityDropEnablesPreemption`) uses a custom policy with 15s
   protection and 15s freshness. If the test timeout (20m) is exceeded,
   it's likely a scheduling or resource issue rather than a timing issue.

6. **Stale resources from previous runs.** Delete leftover RTJs, Workloads,
   and JobSets from previous test runs:

   ```bash
   kubectl -n checkpoint-dev delete rtj --all
   kubectl -n checkpoint-dev delete workloads.kueue.x-k8s.io --all
   kubectl -n checkpoint-dev delete jobsets.jobset.x-k8s.io --all
   ```

**Resolution:**

1. Verify the trainer image is loaded and the Phase 5 profile is installed.
2. Run the e2e tests with verbose output:

   ```bash
   make e2e-phase5 PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE
   ```

3. If individual tests fail, run them in isolation:

   ```bash
   RUN_KIND_E2E=1 PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE \
     go test ./test/e2e -run TestProtectedPriorityBlocksPreemption -v -timeout 20m
   ```

4. Check operator logs during the test for errors:

   ```bash
   kubectl logs <operator-pod> --tail=100 | grep -i "error\|priority\|preempt"
   ```

---

## Quick diagnostic checklist

When investigating Phase 5 issues, run through this checklist:

```bash
# 1. Is the operator running with Phase 5 support?
kubectl logs <operator-pod> | head -5  # Look for phase5Metrics=true

# 2. Does the RTJ reference a valid policy?
kubectl -n checkpoint-dev get rtj <name> -o jsonpath='{.spec.priorityPolicyRef.name}'
kubectl get cpp <policy-name>

# 3. What phase is the RTJ in?
kubectl -n checkpoint-dev get rtj <name> -o jsonpath='{.status.phase}'

# 4. What is the current priority shaping state?
make phase5-inspect-priority PHASE5_LOW_RTJ_NAME=<name>

# 5. Is the Workload priority correct?
make phase5-inspect-workload PHASE5_RTJ_NAME=<name>

# 6. Are checkpoints arriving?
make phase5-inspect-checkpoints PHASE5_RTJ_NAME=<name>

# 7. Is the policy configured correctly?
make phase5-inspect-policy PHASE5_RTJ_NAME=<name>

# 8. What do the metrics say?
curl -s http://localhost:8080/metrics | grep priority
```
