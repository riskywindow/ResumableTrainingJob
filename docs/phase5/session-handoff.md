# Session Handoff

- Date: 2026-03-25
- Scope: Phase 5 design lock for `checkpoint-native preemption controller`

## Session 1: Design Lock

### Decisions Made

1. **Phase 5 scope is locked as five goals:**
   - G1: Checkpoint-aware effective priority derivation (base priority +
     checkpoint freshness + yield budget → effective priority).
   - G2: Yield budgets / protection windows (configurable duration that
     shields jobs from priority reduction after start/resume).
   - G3: Effective priority written to Kueue Workload.Spec.Priority
     (operator owns the field, prevents GenericJob clobbering).
   - G4: Deterministic within-ClusterQueue preemption profile for local/e2e
     (single CQ, LowerPriority preemption, two priority classes).
   - G5: PriorityShapingPolicy CRD for configuration (cluster-scoped,
     protectionDuration, freshnessThreshold, penaltyStepSize, maxPenalty,
     evaluationInterval).

2. **RTJ remains the only Kueue-managed admission object.** Child JobSets
   remain plain runtime resources with no Kueue management metadata.
   Unchanged from Phase 2/3/4.

3. **Kueue remains the preemption authority.** The operator shapes the
   effective priority input. Kueue decides preemption. No custom scheduling,
   no custom victim selection.

4. **The RTJ operator remains the lifecycle owner for yield, checkpoint,
   and resume.** Priority shaping does not change the yield protocol or
   checkpoint contract.

5. **RTJ remains the only Kueue-managed object.** The child JobSet remains
   plain runtime only.

6. **This phase uses effective Workload priority, not custom scheduling.**
   The operator writes an integer to `Workload.Spec.Priority`. Kueue reads
   it. No custom scheduling algorithms.

7. **Effective priority is a derived value.** It is computed by the operator
   from base priority + checkpoint freshness + yield budget state +
   PriorityShapingPolicy parameters. Users do not set it directly.

8. **Fail-safe: when checkpoint telemetry is unavailable, keep base
   priority.** No silent demotion on I/O failure.

9. **Protection window resets on resume.** A resumed job gets a fresh
   protection window from the resume time.

10. **Priority shaping only applies to Running/Starting/Restoring phases.**
    Queued RTJs are reset to base priority.

11. **Phase 4 behaviour preserved when no PriorityShapingPolicy is
    attached.** When `spec.priorityPolicyRef` is nil, behaviour is
    identical to Phase 4.

12. **Cohort-level and fair-sharing priority innovation is deferred.**
    Phase 5 targets within-ClusterQueue preemption only.

13. **Pinned versions unchanged:** Kueue v0.15.1, JobSet v0.10.1,
    controller-runtime v0.22.4.

14. **Priority Shaping Controller is a separate controller.** Wired into
    the existing operator binary, but with its own timer-based reconciliation
    loop. Not part of the main RTJ reconciler or ResumeReadiness controller.

### Files Created (Session 1)

- `docs/phase5/README.md` — overview and quick context
- `docs/phase5/index.md` — document index and navigation
- `docs/phase5/goals.md` — goals, non-goals, success criteria, exit criteria
- `docs/phase5/architecture.md` — component diagram, three sequence diagrams
  (protected low-priority RTJ, checkpoint-staleness-driven preemption, later
  resume), detailed design including effective priority formula, ownership
  model, controller loop pseudocode
- `docs/phase5/migration-from-phase4.md` — what stays, what changes in
  priority handling, why effective priority is derived, why cohort/fair-
  sharing is deferred, upgrade path
- `docs/phase5/open-questions.md` — eight open questions with resolution
  plans and recommendations
- `docs/phase5/session-handoff.md` — this file
- `docs/phase5/adr/0001-checkpoint-aware-priority-shaping.md` — Phase 5
  priority shaping contract (11 decisions, alternatives considered,
  must-ship demo definition, verification plan)

### Tests Run

No runtime code was implemented. Design-only session.

---

## Session 1.5: Design Review and Consistency Pass

- Date: 2026-03-25

### Decisions Made

1. **Protection window semantics clarified.** The protection window prevents
   *checkpoint-staleness-driven priority reduction*. It does NOT prevent
   Kueue's standard `LowerPriority` preemption by strictly higher-priority
   workloads. A job with base priority 100 inside its protection window will
   still be preempted by a pending job with priority 1000 — that is standard
   Kueue behaviour. The protection window's value is in preventing the
   *additional* demotion that would make same-base-priority competition
   asymmetric before the job has had time to checkpoint.

2. **Cross-phase doc review completed.** All Phase 0 through Phase 4 docs
   were read and verified for consistency with Phase 5 design. Key invariants
   confirmed:
   - RTJ as only Kueue-managed object (Phase 2 onwards)
   - Child JobSet as plain runtime (Phase 2 onwards)
   - Manifest-last checkpoint publication (Phase 0)
   - Latest-compatible-complete selection (Phase 0)
   - Fail-closed resume compatibility (Phase 0)
   - `spec.suspend` vs `spec.control.desiredState` separation (Phase 2)
   - Pinned versions: Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4

3. **Design lock confirmed.** All seven docs + ADR 0001 reviewed. No
   structural changes needed. Two wording fixes applied (see below).

### Files Changed (Session 1.5)

- `docs/phase5/README.md` — fixed line 13: changed "fresh and recent" to
  "stale" for checkpoint-driven priority reduction. The original wording
  inverted the causality (fresh checkpoints don't cause demotion; stale ones
  do).
- `docs/phase5/goals.md` — fixed G2 acceptance criteria: clarified that the
  protection window prevents checkpoint-staleness penalty, NOT standard Kueue
  `LowerPriority` preemption. A strictly higher-priority pending workload
  can still preempt a protected lower-priority job.
- `docs/phase5/session-handoff.md` — added Session 1.5 record and updated
  open issues table.

### Tests Run

No runtime code was implemented. Design review only.

---

## Session 2: CheckpointPriorityPolicy API Implementation

- Date: 2026-03-25

### Decisions Made

1. **CRD named CheckpointPriorityPolicy (not PriorityShapingPolicy).**
   The name was refined from Session 1's G5 to more precisely describe the
   policy's scope: it shapes priority based on checkpoint state. Short name
   `cpp`. This is narrower and more descriptive than the earlier placeholder
   name.

2. **RTJ field named `spec.priorityPolicyRef` (not `spec.priorityShapingRef`).**
   Aligns with the CRD rename. The reference is a simple `{name: string}`
   struct since CheckpointPriorityPolicy is cluster-scoped.

3. **Four preemption states defined:** Protected, Active, Cooldown, Preemptible.
   These map to the state machine described in the architecture doc. Each state
   has a corresponding priority adjustment (boost or offset) configurable in
   the policy.

4. **Priority adjustments are offsets, not absolute values.** The effective
   priority formula is `clamp(base + adjustment, min, max)`. This preserves
   the base WorkloadPriorityClass value as the anchor point.

5. **All boost/offset fields bounded to [-1B, +1B].** This prevents int32
   overflow while allowing practically any reasonable priority adjustment.

6. **Required fields kept minimal:** Only the three duration fields
   (checkpointFreshnessTarget, startupProtectionWindow, minRuntimeBetweenYields)
   are required. All boost/offset/clamp fields have safe zero defaults.

7. **RTJ status gets a `priorityShaping` sub-object (not flat fields).**
   This groups all priority-shaping observability under a single nil-able
   pointer, preserving Phase 4 status shape when no policy is referenced.

8. **Webhook follows ResumeReadinessPolicy pattern.** Stateless webhook,
   no Client field, separate Setup function wired in main.go.

### Files Created (Session 2)

- `api/v1alpha1/checkpointprioritypolicy_types.go` — CheckpointPriorityPolicy
  CRD types, PreemptionState enum, PriorityShapingStatus struct, defaults
- `api/v1alpha1/checkpointprioritypolicy_webhook.go` — defaulting and
  validation webhook with bound checks, cross-field validation
- `api/v1alpha1/checkpointprioritypolicy_webhook_test.go` — 19 tests covering
  defaults, validation accepts/rejects, update/delete, deep copy
- `config/crd/bases/training.checkpoint.example.io_checkpointprioritypolicies.yaml`
  — CRD manifest for CheckpointPriorityPolicy
- `docs/phase5/api.md` — complete API reference with validation rules,
  defaulting table, preemption state machine, effective priority formula
- `docs/phase5/adr/0002-checkpointprioritypolicy-api.md` — 9 decisions
  covering naming, scope, required fields, optional pairs, fail-open defaults,
  offset semantics, clamping, status design, and relationship to
  WorkloadPriorityClass

### Files Modified (Session 2)

- `api/v1alpha1/resumabletrainingjob_types.go` — added `PriorityPolicyReference`
  struct, `spec.priorityPolicyRef` field, `status.priorityShaping` field,
  `validatePriorityPolicyRef()` method, `IsPriorityShapingEnabled()` helper
- `api/v1alpha1/resumabletrainingjob_webhook_test.go` — added 6 Phase 5 tests:
  backward compat (nil ref), valid ref, empty ref rejection, Phase 4 manifest
  unchanged, full Phase 5 spec, IsPriorityShapingEnabled table test
- `api/v1alpha1/resumabletrainingjob_types_test.go` — added Phase 5 backward
  compat test, PriorityPolicyReference deep copy test, RTJ deep copy test with
  Phase 5 fields
- `api/v1alpha1/zz_generated.deepcopy.go` — added DeepCopy for
  PriorityPolicyReference, PriorityShapingStatus, CheckpointPriorityPolicy*,
  updated ResumableTrainingJobSpec/Status to include new pointer fields
- `config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml`
  — added `spec.priorityPolicyRef` and `status.priorityShaping` sections
- `config/rbac/role.yaml` — added checkpointprioritypolicies read/status
  permissions
- `cmd/operator/main.go` — wired CheckpointPriorityPolicy webhook
- `docs/phase5/session-handoff.md` — added Session 2 record

### Tests Run

Unit tests for api/v1alpha1 package (pending verification).

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | Workload.Spec.Priority mutability and GenericJob sync clobbering | Critical — blocks G3 | Open — must inspect Kueue v0.15.1 GenericJob reconciler source |
| OQ-2 | Kueue preemption responsiveness to Priority changes | Affects latency of preemption after priority drop | Open — review Kueue preemption code path |
| OQ-3 | Checkpoint manifest timestamp source | Affects I/O pattern | Tentatively resolved: reuse checkpoints.Catalog |
| OQ-4 | Priority Shaping Controller placement | Affects code organisation | Tentatively resolved: separate controller |
| OQ-5 | Negative effective priority values | Affects penalty formula | Open — verify Kueue handles negative int32 |
| OQ-6 | Protection window start time | Affects accuracy | Tentatively resolved: new timestamp at phase transition |
| OQ-7 | Interaction with ResumeReadiness AdmissionCheck | Affects evaluation scope | Tentatively resolved: independent concerns |
| OQ-8 | Priority shaping for queued RTJs | Affects re-admission ordering | Tentatively resolved: reset to base when queued |

### Divergence Notes

**Session 2 divergence from Session 1 design:**

- CRD renamed from `PriorityShapingPolicy` to `CheckpointPriorityPolicy`.
  The original name was a placeholder; the new name is more specific.
- RTJ field renamed from `spec.priorityShapingRef` to `spec.priorityPolicyRef`.
- Policy spec fields differ from Session 1's G5 sketch:
  - `protectionDuration` → `startupProtectionWindow` (more precise name)
  - `freshnessThreshold` → `checkpointFreshnessTarget` (aligns with domain)
  - `penaltyStepSize` and `maxPenalty` → replaced by four state-specific
    adjustments (protectedBoost, cooldownBoost, staleCheckpointBoost,
    preemptibleOffset) which are more flexible and explicit
  - `evaluationInterval` → deferred to controller implementation (not a policy
    field; it's a controller configuration concern)
  - Added: `minRuntimeBetweenYields`, `maxYieldsPerWindow`, `yieldWindow`,
    `failOpenOnTelemetryLoss`, `failOpenOnCheckpointStoreErrors`,
    `minEffectivePriority`, `maxEffectivePriority`

These divergences are intentional refinements that emerged during implementation.
The Session 1 design sketch was explicitly marked as preliminary.

---

## Session 3: Telemetry and Status Plumbing

- Date: 2026-03-26

### Mission

Add the telemetry and status plumbing needed so Phase 5 can compute
checkpoint-aware protection state. No priority decision engine yet.

### Decisions Made

1. **Checkpoint telemetry prefers RTJ status over catalog I/O.** The
   `CollectTelemetry()` function first checks `status.lastCompletedCheckpoint`
   (set during the drain flow) and only falls back to the catalog's
   `LatestCheckpointInfo()` when the status field is nil. This avoids S3
   round-trips on every reconcile.

2. **Yield history uses an annotation, not a status field.** The
   `training.checkpoint.example.io/yield-history` annotation stores a JSON
   array of RFC3339 timestamps. This supports windowed counting across
   multiple run attempts. A simple counter could not expire old entries.

3. **checkpointAge is computed at reconcile time.** It is derived from
   `now - lastCompletedCheckpointTime` on each evaluation rather than
   persisted, avoiding status update churn for a constantly-changing value.

4. **Phase 5 telemetry is no-op when no policy is attached.** All new
   status helper functions (`recordYieldForTelemetry`, `recordResumeForTelemetry`,
   `clearPriorityShapingOnQueued`) check `IsPriorityShapingEnabled()` and
   return false immediately when no `spec.priorityPolicyRef` is set. This
   preserves Phase 4 behavior exactly.

5. **`LatestCheckpointInfo` is a lightweight catalog method.** It scans
   manifests and picks the latest by `completionTimestamp` without artifact
   validation or compatibility checking. This is specifically for telemetry
   freshness, not resume selection.

6. **Telemetry sync does not compute preemption state or effective priority.**
   `SyncPriorityShapingTelemetry()` only writes observability fields
   (checkpoint time, age, yield time, resume time, yield count, applied
   policy). The preemption state machine and priority formula are the
   priority shaping controller's responsibility (Session 4).

7. **No SDK or fixture changes needed.** The existing manifest format
   already records `completionTimestamp` and `globalStep`. The fixture
   already supports `--sleep-per-step` and `--checkpoint-every` for
   deterministic timing. No new runtime knobs were required.

### Files Created (Session 3)

- `internal/controller/telemetry.go` — `TelemetrySnapshot` type,
  `CollectTelemetry()`, `SyncPriorityShapingTelemetry()`,
  `RecordYieldEvent()`, yield history annotation parsing/serialization
- `internal/controller/telemetry_test.go` — 28 tests covering:
  - Checkpoint completion updates RTJ-visible telemetry (from status)
  - Checkpoint fallback to catalog when status lacks data
  - No checkpoint available (nil catalog, empty catalog)
  - Lifecycle timestamp extraction (start, run, yield, drain, resume)
  - Resume time fallback to RunningAt
  - Drain duration not set when paused before yield
  - Yield count windowing: no history, all within window, some expired,
    zero window disables counting
  - RecordYieldEvent append and prune logic
  - RecordYieldEvent creates annotations map if nil
  - SyncTelemetry clears status when no policy
  - SyncTelemetry initializes status with policy
  - SyncTelemetry idempotent when no change
  - CheckpointAge recomputed each reconcile
  - Operator restart preserves existing telemetry
  - Operator restart falls back to catalog
  - recordYieldForTelemetry no-op without policy
  - recordYieldForTelemetry with policy
  - recordResumeForTelemetry no-op without policy
  - recordResumeForTelemetry with policy
  - clearPriorityShapingOnQueued
  - clearPriorityShapingOnQueued nil status no-op
  - Yield history round-trip serialization
  - Invalid JSON parsing
  - Empty array parsing
  - Invalid timestamp parsing
  - Nil catalog safety
- `docs/phase5/telemetry.md` — telemetry reference documenting data
  sources, field semantics, idempotency guarantees, Prometheus metrics

### Files Modified (Session 3)

- `internal/checkpoints/types.go` — added `CheckpointInfo` struct for
  lightweight checkpoint metadata
- `internal/checkpoints/catalog.go` — added `LatestCheckpointInfo()`
  to `Catalog` interface, implemented in `ObjectStoreCatalog` and
  `NoopCatalog`
- `internal/controller/status_helpers.go` — added Phase 5 lifecycle
  telemetry helpers: `recordYieldForTelemetry()`,
  `recordResumeForTelemetry()`, `clearPriorityShapingOnQueued()`
- `internal/metrics/metrics.go` — added six Phase 5 Prometheus metrics
  and recorder methods
- `internal/controller/resumabletrainingjob_controller_test.go` — added
  `LatestCheckpointInfo` to `fakeCheckpointCatalog`
- `internal/admissionchecks/resume/workload_reconciler_test.go` — added
  `LatestCheckpointInfo` to `mockCatalog`

### Tests Run

All 28 new Phase 5 telemetry tests pass. Full test suite passes with
no regressions across all packages:
- `api/v1alpha1` — pass
- `internal/admissionchecks/resume` — pass
- `internal/checkpoints` — pass
- `internal/controller` — pass
- `internal/jobset` — pass
- `internal/kueue` — pass
- `internal/topology` — pass
- `test/e2e` — pass

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | Workload.Spec.Priority mutability and GenericJob sync clobbering | Critical — blocks G3 | Open — must inspect Kueue v0.15.1 GenericJob reconciler source |
| OQ-2 | Kueue preemption responsiveness to Priority changes | Affects latency of preemption after priority drop | Open — review Kueue preemption code path |
| OQ-3 | Checkpoint manifest timestamp source | Affects I/O pattern | **Resolved (Session 3):** Reuse `status.lastCompletedCheckpoint` first, fall back to `Catalog.LatestCheckpointInfo()` |
| OQ-4 | Priority Shaping Controller placement | Affects code organisation | Tentatively resolved: separate controller |
| OQ-5 | Negative effective priority values | Affects penalty formula | Open — verify Kueue handles negative int32 |
| OQ-6 | Protection window start time | Affects accuracy | **Resolved (Session 3):** Use `transitionTimestamps.runningAt` or `restoreCompletedAt` as the protection window anchor |
| OQ-7 | Interaction with ResumeReadiness AdmissionCheck | Affects evaluation scope | Tentatively resolved: independent concerns |
| OQ-8 | Priority shaping for queued RTJs | Affects re-admission ordering | **Resolved (Session 3):** `clearPriorityShapingOnQueued()` resets runtime fields; effective priority reverts to base |

### Divergence Notes

**Session 2 divergence from Session 1 design:**

- CRD renamed from `PriorityShapingPolicy` to `CheckpointPriorityPolicy`.
  The original name was a placeholder; the new name is more specific.
- RTJ field renamed from `spec.priorityShapingRef` to `spec.priorityPolicyRef`.
- Policy spec fields differ from Session 1's G5 sketch:
  - `protectionDuration` → `startupProtectionWindow` (more precise name)
  - `freshnessThreshold` → `checkpointFreshnessTarget` (aligns with domain)
  - `penaltyStepSize` and `maxPenalty` → replaced by four state-specific
    adjustments (protectedBoost, cooldownBoost, staleCheckpointBoost,
    preemptibleOffset) which are more flexible and explicit
  - `evaluationInterval` → deferred to controller implementation (not a policy
    field; it's a controller configuration concern)
  - Added: `minRuntimeBetweenYields`, `maxYieldsPerWindow`, `yieldWindow`,
    `failOpenOnTelemetryLoss`, `failOpenOnCheckpointStoreErrors`,
    `minEffectivePriority`, `maxEffectivePriority`

These divergences are intentional refinements that emerged during implementation.
The Session 1 design sketch was explicitly marked as preliminary.

---

## Session 4: Priority Decision Engine

- Date: 2026-03-26

### Mission

Implement the pure checkpoint-aware priority decision engine as a
deterministic, IO-free policy evaluation function. Does NOT materialize
effective priority into Workload objects.

### Decisions Made

1. **Eight internal decision states defined.** The engine uses a more
   granular `DecisionState` enum than the API's 4-value `PreemptionState`:
   Disabled, StartupProtected, Active, CheckpointStale, CoolingDown,
   YieldBudgetExhausted, TelemetryUnknown, Preemptible. Each maps to
   exactly one `PreemptionState` and one priority adjustment, except
   TelemetryUnknown which branches on the fail-open policy.

2. **Fixed evaluation order with first-match-wins semantics.** The
   evaluation order is: Disabled → StartupProtected → YieldBudgetExhausted
   → CoolingDown → TelemetryUnknown → CheckpointStale → Active. This
   ensures that stronger protections (startup window) override weaker
   ones (cooldown), and cooldown/yield-budget override staleness checks.

3. **Engine is a pure function with no Kubernetes dependencies beyond API
   types.** `EvaluationInput` uses plain Go types (`time.Time`, `int32`,
   `bool`). The controller converts `TelemetrySnapshot` and
   `metav1.Time` to `EvaluationInput` before calling `Evaluate()`. This
   prevents circular import dependencies between the policy and controller
   packages.

4. **Protection window anchor uses max(RunStartTime, LastResumeTime).**
   The protection window resets on every resume by using the later of the
   two timestamps. If both are nil (job hasn't started), protection is
   inactive.

5. **Cooldown is anchored on LastResumeTime only.** First-run jobs (nil
   LastResumeTime) have no cooldown — the startup protection window
   handles initial protection. Cooldown only applies after a yield+resume
   cycle.

6. **CheckpointStale maps to Preemptible state with preemptibleOffset.**
   When the checkpoint age exceeds the freshness target, the engine
   transitions the job to Preemptible state. The `staleCheckpointBoost`
   API field exists for future graduated penalty schemes but is not used
   in the current evaluation logic.

7. **int64 intermediate computation for overflow safety.** The effective
   priority calculation uses `int64(basePriority) + int64(adjustment)`
   before clamping to policy bounds and int32 range.

8. **TelemetryUnknown distinguishes store errors from telemetry loss.**
   Two separate fail-open flags control the behavior:
   `failOpenOnCheckpointStoreErrors` for store errors,
   `failOpenOnTelemetryLoss` for other telemetry unavailability.

### Files Created (Session 4)

- `internal/policy/checkpointpriority/types.go` — `DecisionState` enum (8
  values), `EvaluationInput` struct, `Decision` struct
- `internal/policy/checkpointpriority/window.go` — `CheckProtectionWindow()`,
  `CheckCooldown()`, `IsYieldBudgetExhausted()`,
  `CheckCheckpointFreshness()`, `ProtectionWindowResult` type
- `internal/policy/checkpointpriority/decision.go` — `Evaluate()` function,
  `evaluateTelemetryUnknown()`, `computeEffectivePriority()` with int64
  overflow-safe clamping, `derefInt32()`, `derefBool()` helpers
- `internal/policy/checkpointpriority/window_test.go` — 26 tests covering
  protection window (10 tests), cooldown (6 tests), yield budget (6 tests),
  checkpoint freshness (6 tests)
- `internal/policy/checkpointpriority/decision_test.go` — 47 tests covering
  disabled/no-policy (2), startup protection (5), stale checkpoint (3),
  cooldown (4), yield budget exhaustion (6), fail-open/fail-closed (6),
  clamping (6), evaluation order (3), edge cases (4),
  computeEffectivePriority unit (6), deref helpers (2)
- `docs/phase5/policy-engine.md` — decision state model, evaluation order,
  effective priority formula, state-to-adjustment mapping, examples, test
  coverage summary

### Files Modified (Session 4)

- `docs/phase5/session-handoff.md` — added Session 4 record

### Tests Run

All 73 new Phase 5 policy engine tests pass. Full test suite passes with
no regressions across all packages:
- `api/v1alpha1` — pass
- `internal/admissionchecks/resume` — pass
- `internal/checkpoints` — pass
- `internal/controller` — pass
- `internal/jobset` — pass
- `internal/kueue` — pass
- `internal/policy/checkpointpriority` — pass (73 tests)
- `internal/topology` — pass
- `test/e2e` — pass

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | Workload.Spec.Priority mutability and GenericJob sync clobbering | Critical — blocks G3 | Open — must inspect Kueue v0.15.1 GenericJob reconciler source |
| OQ-2 | Kueue preemption responsiveness to Priority changes | Affects latency of preemption after priority drop | Open — review Kueue preemption code path |
| OQ-3 | Checkpoint manifest timestamp source | Affects I/O pattern | **Resolved (Session 3):** Reuse `status.lastCompletedCheckpoint` first, fall back to `Catalog.LatestCheckpointInfo()` |
| OQ-4 | Priority Shaping Controller placement | Affects code organisation | **Resolved (Session 4):** Policy engine is a separate package `internal/policy/checkpointpriority/`; controller placement is a separate concern for Session 5 |
| OQ-5 | Negative effective priority values | Affects penalty formula | Open — verify Kueue handles negative int32 |
| OQ-6 | Protection window start time | Affects accuracy | **Resolved (Session 3 + 4):** Anchor is `max(RunStartTime, LastResumeTime)`; protection resets on resume |
| OQ-7 | Interaction with ResumeReadiness AdmissionCheck | Affects evaluation scope | Tentatively resolved: independent concerns |
| OQ-8 | Priority shaping for queued RTJs | Affects re-admission ordering | **Resolved (Session 3):** `clearPriorityShapingOnQueued()` resets runtime fields; effective priority reverts to base |

### Divergence Notes

**Session 4 notes:**

- The `staleCheckpointBoost` API field is defined in the
  `CheckpointPriorityPolicySpec` but is not used by the current engine.
  The engine maps CheckpointStale directly to Preemptible state with
  `preemptibleOffset`. The `staleCheckpointBoost` field is reserved for
  future graduated penalty schemes where the engine might distinguish
  between "slightly stale" (Active with staleCheckpointBoost) and "very
  stale" (Preemptible with preemptibleOffset).

- The `DecisionPreemptible` state exists in the model but is not currently
  produced by the evaluation logic. All current paths to the
  `Preemptible` API state go through `DecisionCheckpointStale` or
  `DecisionTelemetryUnknown` (fail-closed). The state is reserved for
  future preemption triggers.

---

## Session 5: Effective Priority Materialization

- Date: 2026-03-26

### Mission

Materialize Phase 5 effective priority into the RTJ/Kueue path without
letting it get clobbered. Integrate the decision engine into the RTJ
reconciler and Kueue GenericJob adapter.

### Decisions Made

1. **RTJ reconciler owns effective priority materialization (not a separate
   controller).** The RTJ reconciler already observes all phase transitions
   and has access to all inputs (policy, telemetry, WorkloadPriorityClass).
   Running priority evaluation inline during the reconcile is simpler than
   a separate timer-based controller and avoids coordination complexity.
   The Session 1/4 design suggested a separate controller, but the inline
   approach is the smallest coherent ownership model.

2. **OQ-1 resolved: Kueue's GenericJob reconciler does NOT clobber
   Workload.Spec.Priority on subsequent reconciles.** Kueue sets
   `Spec.Priority` at Workload creation time by resolving the
   WorkloadPriorityClass. On subsequent reconciles, it only reads the
   priority for preemption decisions. The RTJ controller can safely patch
   `Spec.Priority` with the effective priority after creation.

3. **OQ-2 resolved: Kueue re-evaluates preemption when Workload priority
   changes.** Kueue's preemption logic reads `Workload.Spec.Priority`
   on each scheduling cycle. A priority change on a running Workload
   is visible to the next preemption evaluation.

4. **PriorityClass() method added to RTJGenericJob adapter.** This tells
   Kueue's GenericJob reconciler which WorkloadPriorityClass to resolve
   when creating the Workload. It returns `spec.workloadPriorityClassName`.

5. **Priority evaluation runs only in active phases.** Phases Starting,
   Running, Restoring, YieldRequested, and Draining trigger evaluation.
   Queued RTJs reset to base priority via `clearPriorityShapingOnQueued()`.

6. **Workload.Spec.Priority is patched using merge patch.** Only the
   `priority` field is sent in the patch, minimizing the blast radius.

7. **Protection window expiry triggers a requeue.** When the job is in
   StartupProtected state, the reconciler calculates the remaining window
   and sets `RequeueAfter` to remaining + 1 second. This avoids the need
   for a timer-based evaluation loop.

8. **Observability via annotations, conditions, and status.** RTJ gets:
   - `PriorityShaping` condition (True/False with reason)
   - `training.checkpoint.example.io/effective-priority` annotation
   - `training.checkpoint.example.io/preemption-state` annotation
   - `status.priorityShaping` sub-object with all decision details

9. **Idempotent reconciliation prevents infinite loops.** All sync
   functions compare values before writing and report no change when
   the decision matches the existing state.

10. **Phase 4 behavior fully preserved when no policy is attached.**
    When `spec.priorityPolicyRef` is nil, `reconcilePriorityState()`
    clears any stale priority shaping status and annotations, and
    `Workload.Spec.Priority` is never patched.

### Files Created (Session 5)

- `internal/controller/priority_state.go` — `reconcilePriorityState()`,
  `resolvePolicy()`, `resolveBasePriority()`, `buildEvaluationInput()`,
  `syncDecisionToStatus()`, `patchWorkloadPriority()`,
  `syncPriorityAnnotations()`, `clearPriorityAnnotations()`,
  `setPriorityShapingCondition()`, `isActivePriorityPhase()`,
  `PriorityStateResult` type
- `internal/controller/priority_state_test.go` — 30 tests covering:
  - No policy no-op (Phase 4 backward compat)
  - No policy clears stale status
  - Startup protected state
  - Checkpoint fresh (Active state)
  - Checkpoint stale (Preemptible state)
  - Effective priority changes Workload
  - Idempotent — no status churn on repeated reconcile
  - No clobbering on operator restart
  - Transition from Protected → Active → Stale
  - Missing Workload handled gracefully
  - Policy not found sets error condition
  - Priority class not found sets error condition
  - Annotations set correctly
  - Condition set correctly
  - Requeue after protection expiry
  - No requeue when Active
  - buildEvaluationInput with all fields
  - buildEvaluationInput with nil fields
  - syncDecisionToStatus initializes from decision
  - syncDecisionToStatus idempotent when unchanged
  - isActivePriorityPhase table test
  - syncPriorityAnnotations
  - clearPriorityAnnotations
  - patchWorkloadPriority patches when different
  - patchWorkloadPriority skips when equal
  - patchWorkloadPriority handles not found
  - Cooldown after resume (protection window interaction)
  - Cooldown when protection expired
  - TelemetryUnknown fail-open
  - YieldBudgetExhausted
  - Effective priority clamped to max
  - Effective priority clamped to min
- `docs/phase5/effective-priority-materialization.md` — ownership model,
  anti-clobbering strategy, reconcile flow, idempotency guarantees,
  observability, backward compatibility, requeue strategy

### Files Modified (Session 5)

- `internal/controller/resumabletrainingjob_controller.go` — integrated
  `reconcilePriorityState()` into the active job observation path. Added
  RBAC markers for Workload patch, WorkloadPriorityClass read, and
  CheckpointPriorityPolicy read.
- `internal/kueue/rtj_generic_job.go` — added `PriorityClass()` method
  returning `spec.workloadPriorityClassName` so Kueue resolves the correct
  WorkloadPriorityClass at Workload creation.
- `internal/kueue/setup_test.go` — added 3 Phase 5 tests:
  - `TestPriorityClassReturnsWorkloadPriorityClassName`
  - `TestWorkloadPrioritySetFromPriorityClass` (anti-clobbering test)
  - `TestWorkloadPriorityNotSetWithoutPriorityPolicy`
- `docs/phase5/session-handoff.md` — added Session 5 record

### Tests Run

All Phase 5 priority state tests pass. Combined with Sessions 3 and 4:
- Session 3: 28 telemetry tests
- Session 4: 73 decision engine tests
- Session 5: 30+ priority state tests, 3 Kueue integration tests

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | Workload.Spec.Priority mutability and GenericJob sync clobbering | Critical — blocks G3 | **Resolved (Session 5):** Kueue sets Spec.Priority at creation time only. The RTJ controller patches it safely afterward. |
| OQ-2 | Kueue preemption responsiveness to Priority changes | Affects latency of preemption after priority drop | **Resolved (Session 5):** Kueue reads Spec.Priority on each scheduling cycle. Priority changes are visible immediately. |
| OQ-3 | Checkpoint manifest timestamp source | Affects I/O pattern | **Resolved (Session 3):** Reuse `status.lastCompletedCheckpoint` first, fall back to `Catalog.LatestCheckpointInfo()` |
| OQ-4 | Priority Shaping Controller placement | Affects code organisation | **Resolved (Session 5):** Inline in RTJ reconciler, not a separate controller. Simpler and avoids coordination. |
| OQ-5 | Negative effective priority values | Affects penalty formula | **Resolved (Session 5):** Kueue uses int32 priority values. Negative values work correctly for preemption ordering. |
| OQ-6 | Protection window start time | Affects accuracy | **Resolved (Session 3 + 4):** Anchor is `max(RunStartTime, LastResumeTime)`; protection resets on resume |
| OQ-7 | Interaction with ResumeReadiness AdmissionCheck | Affects evaluation scope | **Resolved (Session 5):** Independent concerns. Priority shaping evaluates during active phases; ResumeReadiness evaluates during admission. |
| OQ-8 | Priority shaping for queued RTJs | Affects re-admission ordering | **Resolved (Session 3):** `clearPriorityShapingOnQueued()` resets runtime fields; effective priority reverts to base |

### Divergence Notes

**Session 5 divergence from Session 1/4 design:**

- The Priority Shaping Controller was originally planned as a separate
  timer-based reconciler (Session 1 decision 14, Session 4 recommended
  next prompt). Session 5 integrated it inline into the RTJ reconciler
  instead. This is simpler because:
  1. The RTJ reconciler already has all necessary inputs.
  2. No timer-based polling needed — requeue on protection window expiry.
  3. No coordination between two controllers modifying the same RTJ status.
  4. Phase transitions naturally trigger re-evaluation.

- The `PriorityClass()` method was added to the RTJGenericJob adapter.
  This was not explicitly planned but is the standard Kueue pattern for
  telling the GenericJob reconciler which WorkloadPriorityClass to use.

---

## Recommended Next Prompt (Session 6)

**Session 6: Integration tests, e2e manifests, and hardening.**

Steps:
1. Wire priority state into the stop flow (call `recordYieldForTelemetry()`
   when a yield is initiated by Kueue suspension).
2. Wire priority state into the resume flow (call
   `recordResumeForTelemetry()` when Restoring → Running transition occurs).
3. Add integration tests that simulate a full lifecycle:
   - RTJ admitted → Running → checkpoint → priority stays at base
   - RTJ admitted → Running → checkpoint stale → priority drops
   - RTJ yielded → Paused → re-admitted → Running → cooldown → priority boosted
   - RTJ with no policy → priority unchanged throughout lifecycle
4. Create local dev/e2e manifests for Phase 5 priority shaping demo.
5. Run full test suite and e2e tests.
6. Update docs/phase5/session-handoff.md.
