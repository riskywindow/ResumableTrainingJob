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

```
You are working on Phase 7 Session 4 for the checkpoint-native preemption
controller repo.

Read docs/phase7/session-handoff.md for context (Sessions 1-3).

Session 3 implemented the internal/provisioning observation layer:
- BuildView() builds a LaunchReadinessView from Kueue Workload status
- 63 tests covering classification, parsing, topology, Phase 6 fallback

Now integrate the observation layer into the RTJ controller:

1. Wire BuildView() into the RTJ reconciler to populate the Phase 7
   status sections on each reconciliation:
   - status.launchGate from LaunchReadinessView
   - status.provisioning from Provisioning classification
   - status.capacity from provisioning + admission state
   Do NOT modify the launch/Starting transition yet.

2. Determine ProvisioningACNames from the ClusterQueue's AdmissionCheck
   configuration (or from a configurable controller option).

3. Add unit tests for the status population logic covering:
   - Phase 6 fallback (no provisioning → launchGate=Open, capacity=QuotaOnly)
   - Provisioning pending → launchGate=Blocked
   - Provisioning ready + topology assigned → launchGate=Open
   - Provisioning failed → launchGate=Blocked
   - Multiple ACs with one pending → launchGate=Blocked

4. Resolve OQ2: read the Kueue v0.15.1 source for waitForPodsReady
   eviction condition type/reason and document findings.

5. Update docs/phase7/session-handoff.md with Session 4 results.
```
