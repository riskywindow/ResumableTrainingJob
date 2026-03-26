# ADR 0003: ResumeReadiness AdmissionCheck Policy

- Status: Accepted
- Date: 2026-03-25
- Scope: Phase 4 G3 — Custom AdmissionCheck Controller Scaffold

## Context

Phase 4 G3 requires a custom Kueue AdmissionCheck controller that gates
workload admission until the operator confirms resume-readiness. This
controller needs a parameter object to configure its behavior per cluster.

## Decisions

### D1: Controller Name Convention

**Decision:** Use `training.checkpoint.example.io/resume-readiness`.

**Rationale:** Follows the Kueue convention where built-in controllers use
`kueue.x-k8s.io/<name>` (e.g., `kueue.x-k8s.io/provisioning-request`,
`kueue.x-k8s.io/multikueue`). External controllers use their own API group.
Our API group is `training.checkpoint.example.io`, making the controller name
self-describing and collision-free.

**Alternatives considered:**
- `checkpoint.example.io/resume-readiness` — doesn't match the API group.
- `resume-readiness-controller` — Kueue expects a qualified name.

### D2: Single Binary vs. Separate Binary

**Decision:** Wire the controller into the existing operator binary.

**Rationale:**
- The controller is lightweight (two reconcilers, no external dependencies).
- Shares scheme registration, leader election, and health infrastructure.
- Reduces deployment complexity (one Deployment, one ServiceAccount).
- No strong isolation requirement — the controller reads Workloads and
  AdmissionChecks that the operator already has RBAC for.

**Alternatives considered:**
- Separate binary — adds Dockerfile, Deployment, ServiceAccount, RBAC
  duplication. Justified only if the controller had different scaling needs
  or conflicting dependencies.

### D3: Cluster-Scoped Parameter CRD

**Decision:** `ResumeReadinessPolicy` is cluster-scoped.

**Rationale:** Kueue `AdmissionCheck` is itself cluster-scoped, and the
`spec.parameters` reference uses a bare `name` without namespace. The
parameter object must therefore also be cluster-scoped to be referenceable.
This matches Kueue's own pattern (`ProvisioningRequestConfig` is
cluster-scoped).

### D4: Minimal Policy Surface

**Decision:** The policy covers exactly four fields:

1. `requireCompleteCheckpoint` — whether resume needs a complete checkpoint.
2. `maxCheckpointAge` — maximum acceptable checkpoint age.
3. `failurePolicy` — fail-open vs fail-closed for transient errors.
4. `allowInitialLaunchWithoutCheckpoint` — whether first launch is allowed.

**Rationale:** These are the minimum controls needed for Phase 4. Additional
fields (retry backoff, checkpoint catalog source, topology validation mode)
can be added in future phases without breaking backward compatibility.

### D5: FailClosed Default

**Decision:** `failurePolicy` defaults to `FailClosed`.

**Rationale:** The fail-closed posture is the safe default for production.
If the controller cannot verify checkpoint readiness (e.g., S3 is
unreachable), it should block admission rather than allow a potentially
broken resume. Operators who prefer availability over strict safety can
set `FailOpen`.

### D6: AllowInitialLaunch Default True

**Decision:** `allowInitialLaunchWithoutCheckpoint` defaults to `true`.

**Rationale:** The most common scenario is that the very first launch of a
training job has no prior checkpoint. Blocking initial launches would require
out-of-band checkpoint seeding, which is unusual. The default allows initial
launches to proceed while requiring complete checkpoints for subsequent
resumes.

### D7: Scaffold-First Implementation

**Decision:** The Workload reconciler unconditionally marks checks as Ready
in this session. Actual readiness logic is deferred.

**Rationale:** This scaffolds the controller shape and registration first,
proving the wiring is correct and the controller compiles and registers.
The readiness decision logic depends on checkpoint catalog integration and
topology assignment parsing, which are separate implementation concerns.

### D8: AdmissionCheck Active Condition Pattern

**Decision:** The AdmissionCheck reconciler evaluates Active=True when:
- `spec.parameters` exists and references the correct GVK.
- The referenced `ResumeReadinessPolicy` object exists.

**Rationale:** This matches Kueue's built-in pattern. Kueue's workload
controller checks the Active condition before applying admission checks
to workloads. If Active is False, the workload will not be considered
for admission.

## Consequences

- Cluster administrators can create AdmissionCheck objects referencing
  `training.checkpoint.example.io/resume-readiness` as the controller name.
- The ResumeReadinessPolicy CRD must be installed alongside the RTJ CRD.
- RBAC must grant the operator read access to AdmissionCheck and
  ResumeReadinessPolicy, and write access to AdmissionCheck/status and
  Workload/status.
- Future sessions must implement the actual readiness decision logic
  without changing the controller shape or registration pattern.

## Verification

- [x] `go build ./...` compiles successfully.
- [x] Unit tests verify scheme registration for ResumeReadinessPolicy.
- [x] Unit tests verify AdmissionCheck reconciler sets Active=True/False.
- [x] Unit tests verify Workload reconciler sets CheckState=Ready.
- [x] Unit tests verify non-managed checks are ignored.
- [x] Webhook tests verify defaulting and validation.
