# Phase 7 -- Session Handoff

## Session 1: Design lock

**Date**: 2026-03-30

### Decisions made

1. **Phase 7 scope locked**: capacity-guaranteed launch using Kueue's
   built-in ProvisioningRequest AdmissionCheck, waitForPodsReady
   integration, fake ProvisioningRequest backend for local dev/e2e.

2. **RTJ remains the only Kueue-managed object**. No change to the
   Kueue integration boundary.

3. **No new RTJ spec fields for provisioning**. Provisioning is configured
   at the ClusterQueue level via Kueue's AdmissionCheck mechanism.

4. **Launch gate generalized**: the RTJ operator checks ALL AdmissionChecks
   on the Workload, not just specific ones. This covers ProvisioningRequest
   AC, ResumeReadiness AC (Phase 4), and any future ACs.

5. **Topology second-pass handling**: the launch gate explicitly waits for
   topology assignment when topology is configured, even if all ACs are
   Ready. Conservative design; may be a no-op if Kueue always provides
   topology in the same pass.

6. **waitForPodsReady eviction reuses yield path**: no new yield logic.
   The RTJ operator detects Kueue eviction conditions and enters the
   existing graceful yield path. New status conditions distinguish
   timeout evictions from preemption.

7. **Fake ProvisioningRequest backend is a separate Deployment**: not
   coupled into the RTJ operator binary. Matches real-world topology.

8. **Phase 6 backward compatibility is unconditional**: no feature gate
   required to preserve Phase 6 behavior. Phase 7 features activate only
   when ProvisioningRequest AC is configured on the ClusterQueue.

9. **Multi-cluster Phase 7 is worker-local**: manager cluster is
   unaffected. Worker cluster runs the full Phase 7 path locally.

10. **Pinned Kueue version is v0.15.1**: all design decisions must be
    validated against this version. Divergences from upstream docs (which
    may describe newer versions) must be documented.

### Files changed

| File | Action |
|---|---|
| docs/phase7/README.md | Created |
| docs/phase7/index.md | Created |
| docs/phase7/goals.md | Created |
| docs/phase7/architecture.md | Created |
| docs/phase7/migration-from-phase6.md | Created |
| docs/phase7/open-questions.md | Created |
| docs/phase7/session-handoff.md | Created |
| docs/phase7/adr/0001-capacity-guaranteed-launch.md | Created |

### Tests run

None (documentation-only session).

### Open issues

1. **OQ1**: Kueue v0.15.1 ProvisioningRequest AC maturity -- must audit
   source before implementation.
2. **OQ2**: waitForPodsReady eviction condition format -- must read Kueue
   source.
3. **OQ3**: Topology assignment timing with ProvisioningRequest -- must
   test in e2e.
4. **OQ4**: ProvisioningRequest cleanup on yield/preemption -- must verify.
5. **OQ5**: Fake backend scope -- decided as separate Deployment.
6. **OQ6**: Multi-cluster provisioning status mirroring -- should-ship.
7. **OQ7**: Backoff behavior surfacing -- should-ship.
8. **OQ8**: Feature gate naming -- decide in Session 2.

### Recommended next prompt (consumed by Session 2)

See Session 2 below.

---

## Session 2: Launch gate status API

**Date**: 2026-03-30

### Mission

Add the smallest API/status surface needed for Phase 7 launch-gating,
provisioning visibility, and startup/recovery status.

### Decisions made

1. **Zero new spec fields.** All Phase 7 additions are status-only,
   controller-owned fields. Provisioning is configured at the ClusterQueue
   level (ADR 0001). No per-workload spec knob is needed (ADR 0002).

2. **Four new status sections added:**
   - `status.launchGate` -- aggregate gate state with per-AC summary
   - `status.provisioning` -- provisioning-specific state with request ref
   - `status.startupRecovery` -- startup/recovery lifecycle with eviction reasons
   - `status.capacity` -- derived capacity guarantee indicator

3. **Seven new enum types:**
   - `LaunchGateState` (Open, Blocked, Unknown)
   - `ProvisioningState` (NotConfigured, Pending, Provisioned, Failed)
   - `StartupState` (NotStarted, Starting, Running, StartupTimedOut, RecoveryTimedOut, Evicted)
   - `PodsReadyState` (Unknown, PodsReady, PodsNotReady, NoRuntime)
   - `TopologyGateState` (NotConfigured, Pending, Assigned)
   - `AdmissionCheckState` (Pending, Ready, Retry, Rejected)

4. **Flat JSON field names match requirements list.** Fields like
   `launchGateState`, `provisioningState`, `provisioningAttempt` appear
   directly in the JSON to enable direct `kubectl get -o jsonpath` queries.

5. **Backward compatibility verified.** Phase 6 manifests pass validation
   unchanged. All new status sections are nil by default. Webhook does not
   inject or validate Phase 7 status.

6. **Controller-owned pattern reused.** Follows Phase 4 (`launchReadiness`),
   Phase 5 (`priorityShaping`), Phase 6 (`multiCluster`) patterns.

7. **Manager/worker compatibility preserved.** Worker mode populates all
   four sections. Manager mode may populate `launchGate` and `provisioning`
   only. Worker state visible via existing `multiCluster.remotePhase`.

### Files changed

| File | Action |
|---|---|
| api/v1alpha1/resumabletrainingjob_types.go | Modified: added 7 enum types, 5 struct types, 4 status fields |
| api/v1alpha1/resumabletrainingjob_webhook.go | Unchanged (no spec changes) |
| api/v1alpha1/resumabletrainingjob_webhook_test.go | Modified: added 4 Phase 7 webhook tests |
| api/v1alpha1/resumabletrainingjob_types_test.go | Modified: added 8 Phase 7 types tests |
| api/v1alpha1/zz_generated.deepcopy.go | Modified: added deepcopy for 5 new types, updated status deepcopy |
| config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml | Modified: added Phase 7 status schema |
| docs/phase7/api.md | Created: Phase 7 API reference |
| docs/phase7/adr/0002-launch-gate-status-api.md | Created: ADR for status-only decision |
| docs/phase7/session-handoff.md | Modified: added Session 2 |

### Tests run

All 95+ tests pass:
- `go test ./api/v1alpha1/ -v` -- PASS
- New Phase 7 tests: backward-compatible decoding, deep copy independence,
  controller-owned status preservation, webhook non-injection

### Tests added

**Types tests:**
- `TestPhase6SpecDecodesWithoutPhase7StatusFields` -- backward compat
- `TestPhase6SpecValidatesUnchangedForPhase7` -- full Phase 6 spec passes
- `TestDeepCopyLaunchGateStatus` -- deep copy with map independence
- `TestDeepCopyProvisioningStatus` -- deep copy with ref independence
- `TestDeepCopyStartupRecoveryStatus` -- deep copy with time independence
- `TestDeepCopyCapacityStatus` -- deep copy value independence
- `TestDeepCopyRTJWithPhase7Fields` -- full RTJ deep copy independence
- `TestControllerOwnedPhase7StatusFieldsPreservedOnSpecUpdate` -- status survives spec update

**Webhook tests:**
- `TestWebhookDefaultPreservesPhase6SpecForPhase7` -- defaulting backward compat
- `TestWebhookValidateCreateAcceptsPhase6ManifestForPhase7` -- full Phase 6 spec passes
- `TestWebhookValidateUpdatePreservesPhase7StatusFields` -- status through webhook
- `TestWebhookDefaultDoesNotInjectPhase7Status` -- no status injection

### Open issues

1. **OQ1**: Kueue v0.15.1 ProvisioningRequest AC maturity -- still open.
2. **OQ2**: waitForPodsReady eviction condition format -- still open.
3. **OQ3**: Topology assignment timing -- still open.
4. **OQ4**: ProvisioningRequest cleanup -- still open.
5. **OQ5**: Fake backend scope -- resolved (separate Deployment).
6. **OQ6**: Multi-cluster provisioning status mirroring -- should-ship.
7. **OQ7**: Backoff behavior surfacing -- should-ship.
8. **OQ8**: Feature gate naming -- still open.

### Recommended next prompt

See Session 3 below.

---

## Session 3: Provisioning / Topology Observation Layer

**Date**: 2026-03-31

### Mission

Implement a read-only provisioning/topology observation layer that builds
a compact internal "launch readiness view" from the RTJ's Kueue Workload
status. The view captures all state the RTJ controller needs to decide
whether launching child runtime is safe.

Hard boundaries:
- Do not launch or mutate child JobSets.
- Reuse Kueue's existing Workload and AdmissionCheck surfaces.
- Compatible with pinned Kueue v0.15.1.
- Phase 6 behavior preserved when provisioning is not configured.

### Decisions made

1. **New `internal/provisioning` package created.** Follows the same
   pattern as `internal/topology` and `internal/kueue` -- a focused
   internal package with pure functions and no controller wiring.

2. **BuildView() is the single entry point.** Takes a `*kueuev1beta2.Workload`
   and `ViewOptions`, returns a `*LaunchReadinessView`. The RTJ controller
   will call this in a future session when integrating the launch gate.

3. **Provisioning AC identification via configured names.** The caller
   passes `ProvisioningACNames` (a `map[string]bool`) to identify which
   admission checks are ProvisioningRequest checks. This avoids hardcoding
   AC names and supports any number of provisioning ACs.

4. **ProvisioningRequest reference derived from naming convention.**
   Kueue v0.15.1 names ProvisioningRequests as `{workload}-{check}-{attempt}`.
   The observation layer derives the expected PR name but does not fetch
   the PR resource. The controller must validate the reference on integration.

5. **Five provisioning classifications.** NotConfigured, Pending,
   Provisioned, Failed, Retry. The Retry state (from Kueue CheckStateRetry)
   is surfaced as a distinct internal classification even though the RTJ
   status API maps it to Pending externally.

6. **podSetUpdates deep-copied on parse.** All maps and slices from
   `kueuev1beta2.PodSetUpdate` are deep-copied into `PodSetUpdateEntry`
   to ensure the view is a true snapshot.

7. **Topology second-pass detection.** `SecondPassPending` is true when
   topology is configured on the RTJ but no TopologyAssignment is present
   on any PodSetAssignment, OR when a DelayedTopologyRequest is in Pending
   state. This implements the conservative design from Session 1 Decision 5.

8. **Phase 6 fallback is clean.** When `ProvisioningACNames` is empty and
   `TopologyEnabled` is false, `BuildView()` returns `AllChecksReady: true`
   and `IsLaunchReady()` returns true as soon as quota is reserved.

9. **Kueue v0.15.1 API surface audited.** All types used come from
   `sigs.k8s.io/kueue/apis/kueue/v1beta2`. Key types verified:
   - `AdmissionCheckState.PodSetUpdates` exists with full CRUD fields
   - `PodSetAssignment.DelayedTopologyRequest` exists with Pending/Ready enum
   - `PodSetAssignment.TopologyAssignment` is the compressed slice format
   - `CheckState` has Pending, Ready, Retry, Rejected values

10. **OQ1 partially resolved.** The ProvisioningRequest AdmissionCheck
    types exist in Kueue v0.15.1 (`ProvisioningRequestConfig`,
    `ProvisioningRequestRetryStrategy`, `ProvisioningRequestPodSetUpdates`).
    The observation layer is compatible. Full controller integration will
    complete the resolution.

### Files changed

| File | Action |
|---|---|
| internal/provisioning/requests.go | Created: provisioning classification and PR ref resolution |
| internal/provisioning/requests_test.go | Created: 15 tests |
| internal/provisioning/podsetupdates.go | Created: podSetUpdates parsing and merging |
| internal/provisioning/podsetupdates_test.go | Created: 14 tests |
| internal/provisioning/topology.go | Created: topology view and delayed topology state |
| internal/provisioning/topology_test.go | Created: 14 tests |
| internal/provisioning/view.go | Created: LaunchReadinessView and BuildView entry point |
| internal/provisioning/view_test.go | Created: 20 tests |
| docs/phase7/provisioning-observation.md | Created: field-level documentation |
| docs/phase7/session-handoff.md | Modified: added Session 3 |

### Tests run

All tests pass (63 new + existing suite):
```
go test ./internal/provisioning/ -v -count=1  -- PASS (63 tests)
go test ./...                                  -- PASS (all packages)
```

### Tests added (63 total)

**requests_test.go (15 tests):**
- NotConfigured for nil/empty/unmatched AC names
- Pending, Provisioned, Failed, Retry classification
- Unknown CheckState defaults to Pending
- Multiple ACs with first-match semantics
- FindProvisioningCheckName found/not-found/nil
- ResolveProvisioningRequestRef basic/attempt2/default/empty-workload/empty-check

**podsetupdates_test.go (14 tests):**
- Nil/empty input returns nil
- Single update with all fields (labels, annotations, nodeSelector, tolerations)
- Multiple PodSets
- Deep copy independence
- Nil maps preserved
- Merge: nil, empty, single AC, multi-AC same PodSet, different PodSets
- HasPodSetUpdates true/false/nil

**topology_test.go (14 tests):**
- Not configured with no assignments
- Configured but no/empty assignments (SecondPassPending)
- TopologyAssignment present and assigned
- DelayedTopologyRequest Pending and Ready
- No assignment and no delayed (SecondPassPending)
- Not configured ignores present assignment
- Multiple PodSets with mixed states
- IsTopologyReady: not configured, assigned, not assigned, assigned+pending
- Unknown delayed state defaults to Pending

**view_test.go (20 tests):**
- Nil workload safe defaults
- Phase 6 fallback (no ACs, quota reserved → launch ready)
- Provisioning pending, ready, failed, retry states
- Multiple ACs all ready / one not ready
- podSetUpdates parsed through BuildView
- MergedPodSetUpdates integration
- Topology assigned / pending second pass / delayed pending / not configured
- No admission (quota not reserved)
- Nil receiver: IsLaunchReady, MergedPodSetUpdates
- Full integration (provisioning + topology + multiple ACs + podSetUpdates)
- CheckState normalization table

### Open issues

1. **OQ1**: Partially resolved. ProvisioningRequest types confirmed in
   Kueue v0.15.1. Controller-level integration will complete resolution.
2. **OQ2**: waitForPodsReady eviction condition format -- still open.
   Not needed for observation layer; needed for StartupRecovery integration.
3. **OQ3**: Topology assignment timing with ProvisioningRequest -- still open.
   The observation layer conservatively waits for both AC Ready + topology
   assigned. E2E testing will confirm actual Kueue behavior.
4. **OQ4**: ProvisioningRequest cleanup on yield/preemption -- still open.
5. **OQ5**: Resolved (Session 1).
6. **OQ6**: Multi-cluster provisioning status mirroring -- should-ship.
7. **OQ7**: Backoff behavior -- observation layer captures Retry state.
   Status surfacing deferred to controller integration.
8. **OQ8**: Feature gate naming -- still open.

### Recommended next prompt

See Session 4 below.

---

## Session 4: Provisioning-Aware Launch Gate & podSetUpdates Integration

**Date**: 2026-03-31

### Mission

Integrate the provisioning observation layer (Session 3) into the RTJ
controller's launch gate and render path. Make launch gating provisioning-
aware and topology-second-pass-aware, and apply AdmissionCheck-suggested
podSetUpdates to the child JobSet.

Hard boundaries:
- RTJ remains the only Kueue-managed object.
- Child JobSets remain plain runtime only.
- Do not launch child runtime before the launch gate is fully satisfied.
- Keep all modifications additive; do not overwrite existing selectors/
  tolerations/labels/annotations in unsupported ways.
- Preserve Phase 6 launch behavior when provisioning is not configured.

### Decisions made

1. **Launch gate generalized with Phase 7 provisioning awareness.**
   `evaluateLaunchGates()` now calls `provisioning.BuildView()` to get the
   `LaunchReadinessView` and evaluates gates in order: quota → all ACs →
   topology second pass → topology assignment. The old Phase 4 resume-
   readiness-specific gate is subsumed by the generalized "all ACs Ready"
   check.

2. **`ProvisioningACNames` added to reconciler.** A `map[string]bool` field
   on `ResumableTrainingJobReconciler` identifies which Workload AC names
   are ProvisioningRequest checks. When empty, provisioning is not configured
   and Phase 6 backward compatibility is preserved unconditionally.

3. **podSetUpdates applied additively in render path.** After topology
   injection, `ApplyPodSetUpdates()` merges labels, annotations,
   nodeSelector (conflict-detected), and tolerations (appended, deduplicated)
   from the merged podSetUpdates into the rendered JobSet spec.

4. **Conflict detection is fail-fast.** Before launching, a dry-run apply
   checks for conflicts between podSetUpdates and the existing rendered
   template. Conflicting keys (same key, different value) cause the RTJ to
   transition to PhaseFailed with a clear error message identifying the
   conflicting field, key, existing value, and update value.

5. **Same-value updates are not conflicts.** If a podSetUpdate sets a key
   that already exists with the same value, it is treated as an idempotent
   merge (not a conflict).

6. **Six Phase 7 conditions surfaced.** CapacityPending,
   ProvisioningInProgress, ProvisioningFailed, TopologyPendingSecondPass,
   LaunchReady, LaunchBlockedByConflictingPodSetUpdate.

7. **Phase 7 status sections populated on every gate evaluation.**
   status.launchGate, status.provisioning, and status.capacity are synced
   from the LaunchReadinessView on both blocked and ready gate outcomes.
   They are nil when the gate evaluation path is not entered (Phase 6
   backward compatibility).

8. **LaunchGateResult extended with LaunchView.** The gate result now
   carries the `*provisioning.LaunchReadinessView` so the caller can
   extract podSetUpdates and provisioning state without a second BuildView
   call.

9. **LaunchPlan extended with PodSetUpdates.** `buildLaunchPlan()` extracts
   merged podSetUpdates from the LaunchView and passes them through
   `toRenderInput()` to the render path.

10. **Existing Phase 4 test updated.** The test
    `TestReconcileDoesNotCreateChildJobSetBeforeTopologyAssignment` now
    accepts both `WaitingForTopologyAssignment` and
    `TopologyPendingSecondPass` reasons, since the Phase 7 gate catches
    the topology second-pass scenario before the old Phase 4 check.

### Files changed

| File | Action |
|---|---|
| internal/controller/launch_gate.go | Modified: added Phase 7 provisioning-aware gate with BuildView integration |
| internal/controller/launch_gate_test.go | Created: 7 Phase 7 launch gate tests |
| internal/controller/status_helpers.go | Modified: added 6 Phase 7 status helpers, condition types, comparison functions |
| internal/controller/launch_plan.go | Modified: added PodSetUpdates field, BuildView extraction |
| internal/controller/resumabletrainingjob_controller.go | Modified: added ProvisioningACNames, wired Phase 7 status sync, podSetUpdate conflict check |
| internal/controller/resumabletrainingjob_controller_test.go | Modified: updated topology test for Phase 7 reason |
| internal/jobset/render.go | Modified: added PodSetUpdates to RenderInput, apply step after topology |
| internal/jobset/podsetupdates.go | Created: additive podSetUpdate application with conflict detection |
| internal/jobset/podsetupdates_test.go | Created: 17 podSetUpdates tests |
| docs/phase7/launch-gating.md | Created: Phase 7 launch gating documentation |
| docs/phase7/session-handoff.md | Modified: added Session 4 |

### Tests run

All tests pass across the entire project:
```
go test ./... -count=1  -- PASS (all packages)
```

### Tests added (24 total)

**launch_gate_test.go (7 tests):**
- `TestReconcileDoesNotCreateChildJobSetBeforeProvisioningReady` -- no child while provisioning pending
- `TestReconcileDoesNotCreateChildJobSetBeforeDelayedTopologyAssignment` -- no child while topology second pass pending
- `TestReconcileProvisioningReadyLaunchesWithPodSetUpdates` -- launches with nodeSelector and toleration from podSetUpdates
- `TestReconcileProvisioningFailedBlocksLaunch` -- no child when provisioning rejected
- `TestReconcilePhase6BehaviorPreservedWhenProvisioningAbsent` -- Phase 6 path unchanged
- `TestReconcileConflictingPodSetUpdateFailsClearly` -- conflicting nodeSelector causes PhaseFailed
- `TestReconcileMultipleACsOneNotReadyBlocksLaunch` -- one pending AC blocks with AC summary

**podsetupdates_test.go (17 tests):**
- Additive labels, nodeSelector, annotations, tolerations
- Conflicting nodeSelector fails
- Same-value nodeSelector not a conflict
- Preserves existing with new keys
- No PodSet match is no-op
- Empty updates is no-op
- Coexists with topology injection
- Conflict message format
- Duplicate tolerations deduplicated
- Direct ApplyPodSetUpdates unit test

### Open issues

1. **OQ1**: Resolved. ProvisioningRequest types confirmed in Kueue v0.15.1
   and controller integration completed.
2. **OQ2**: waitForPodsReady eviction condition format -- still open. Not
   needed for launch gating; needed for StartupRecovery integration (Session 5).
3. **OQ3**: Topology assignment timing with ProvisioningRequest -- still open.
   E2E testing will confirm actual Kueue behavior.
4. **OQ4**: ProvisioningRequest cleanup on yield/preemption -- still open.
5. **OQ5**: Resolved (Session 1).
6. **OQ6**: Multi-cluster provisioning status mirroring -- should-ship.
7. **OQ7**: Backoff behavior -- Retry state captured and mapped to Pending
   in status API. Full surfacing deferred.
8. **OQ8**: Feature gate naming -- still open. ProvisioningACNames is
   currently a reconciler field; could be promoted to a feature gate.

### Recommended next prompt

```
You are working on Phase 7 Session 5 for the checkpoint-native preemption
controller repo.

Read docs/phase7/session-handoff.md for context (Sessions 1-4).

Session 4 integrated the provisioning observation layer into the controller:
- Launch gate is provisioning-aware and topology-second-pass-aware
- podSetUpdates applied additively with conflict detection
- 24 new tests, all passing

Now implement waitForPodsReady timeout/recovery semantics:

1. Resolve OQ2: audit Kueue v0.15.1 source for waitForPodsReady eviction
   condition type/reason format. Document findings.

2. Implement StartupRecovery status population:
   - Detect Kueue eviction conditions on the Workload
   - Populate status.startupRecovery with startup/recovery lifecycle
   - Map eviction reasons to StartupState transitions

3. Wire eviction detection into the yield path:
   - When Kueue evicts via waitForPodsReady timeout, enter the existing
     graceful yield path
   - Distinguish timeout evictions from preemption via conditions

4. Add unit tests covering:
   - Normal startup lifecycle (Starting → Running)
   - waitForPodsReady timeout triggers yield
   - Eviction reason captured in status
   - Phase 6 behavior unchanged

5. Update docs/phase7/session-handoff.md with Session 5 results.
```
