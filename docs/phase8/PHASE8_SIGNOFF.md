# Phase 8 Signoff -- Accelerator-Native DRA Device Requests

**Date**: 2026-04-05
**Phase**: 8 of N
**Status**: SIGNED OFF with noted P1 observation-wiring gaps

---

## What Phase 8 can do

### Core capabilities

1. **Native DRA device requests for RTJ.** The RTJ spec gains an optional
   `spec.devices` section declaring per-worker device requirements using
   the Kubernetes DRA vocabulary: device class, device selectors (CEL),
   and count. Multiple claims per RTJ are supported (e.g., GPU + RDMA).

2. **Companion ResourceClaimTemplate lifecycle.** The RTJ operator creates,
   drift-detects, and garbage-collects ResourceClaimTemplate objects that
   match the RTJ's device spec. Templates are owned by the RTJ with
   `controller=true` and survive child JobSet deletion.

3. **Kueue deviceClassMappings-based quota/accounting.** Kueue resolves
   DRA device claims through `deviceClassMappings` on ClusterQueue
   ResourceGroups. No custom quota engine. Quota exhaustion correctly
   blocks admission; freeing quota unblocks it (e2e verified).

4. **DRA-aware child JobSet rendering.** Child JobSet pod templates include
   `spec.resourceClaims` referencing RTJ-managed ResourceClaimTemplates
   and container `resources.claims` entries. Claims are injected only into
   pods with matching container names.

5. **Conservative checkpoint compatibility for device profiles.** Device
   class and selector fingerprint (SHA256) are stored in checkpoint
   manifests. Resume is fail-closed: different device profiles are
   rejected. Downgrade (DRA to non-DRA) is compatible.

6. **Example DRA driver local dev profile.** Self-contained DRA driver
   via DaemonSet publishing ResourceSlice objects. 4 simulated GPUs per
   node on kind v1.33. Zero-build, zero-vendor-driver setup.

7. **Multi-cluster DRA compatibility.** Manager mode suppresses local
   ResourceClaimTemplate creation. Worker-side DRA status is mirrored
   to the manager via Kueue adapter full-status mirror. Verified by
   multi-cluster smoke test.

8. **Full backward compatibility.** When `spec.devices` is nil or
   mode=Disabled, the operator follows the Phase 7 path unchanged. No
   DRA objects created, no claims injected, no device dimensions added
   to checkpoint compatibility.

### Validated by

- **154 Phase 8 unit tests** covering API/webhook, DRA profile/template,
  claim observation, checkpoint compatibility, render, PodSet synthesis,
  and multi-cluster remote status.
- **4 Phase 8 e2e tests**: DRA quota/allocation lifecycle, compatible
  resume, incompatible-profile rejection, multi-cluster DRA smoke.
- **6 Python SDK tests** for manifest fingerprint round-trip.
- **11-check infrastructure smoke test** for local dev environment.
- **6 demo/inspect scripts** and **7 Makefile targets** for operational
  validation.

---

## What remains deferred

### P1 observation-wiring (recommended for Phase 9 Session 1)

These are wiring-only tasks; the underlying logic is implemented and
unit-tested. They affect runtime observability, not correctness.

| Item | Description |
|------|-------------|
| `observeDRAClaimStatus()` | Wire into Reconcile loop for live claim allocation tracking |
| `syncDeviceResumeFingerprint()` | Wire into resume flow for resume-time fingerprint recording |
| Phase 8 metric emission | Wire 8 DRA metrics into controller callsites |

### P2 deferred items

| Item | Reason |
|------|--------|
| DRA + ProvisioningRequest e2e (OQ9) | Orthogonal admission checks; low interaction risk |
| Device failure recovery e2e | Unit-tested; needs DRA driver with fault injection |
| CEL selector validation at webhook | Consistent with K8s CEL handling elsewhere |
| Multi-device-class per claim | Explicitly deferred in Session 1 |
| Shared/manual ResourceClaims | Explicitly deferred in Session 1 |
| Prioritized device alternatives | Explicitly deferred in Session 1 |
| Extended-resource bridge | Explicitly deferred; native DRA is the core path |
| gpuShape/deviceClassName reconciliation | Both fields coexist independently for Phase 8 |

---

## Main known risks

### R1: DRA API stability (LOW)

Phase 8 uses `k8s.io/api/resource/v1beta1` (k8s.io/api@v0.34.2). DRA is
beta in Kubernetes 1.33-1.34. A GA promotion to `resource.k8s.io/v1` may
require import path changes. Mitigation: the DRA types used
(ResourceClaimTemplate, ResourceClaim, DeviceRequest, DeviceSelector) are
stable in shape across beta versions. Migration is mechanical.

### R2: Self-contained DRA driver limitations (MEDIUM)

The dev environment DRA driver publishes ResourceSlice objects via API
server, not through the kubelet DRA plugin interface. This means device
allocation is not enforced at the node level -- pods can schedule without
actual device binding. This is sufficient for operator-level integration
testing but does not validate kubelet-level device lifecycle. Mitigation:
production deployments must use real DRA drivers (vendor-provided).

### R3: Observation wiring gaps (LOW)

The three P1 wiring gaps (G1, G2, G3) mean that DRA claim status
observation, resume fingerprint recording, and DRA metrics are not active
at runtime. This does not affect correctness or safety -- DRA allocation,
Kueue accounting, and checkpoint compatibility all function correctly
through their independent paths. The gaps affect operator observability
and debugging experience.

### R4: Kueue deviceClassMappings configuration dependency (MEDIUM)

DRA quota accounting requires the cluster admin to correctly configure
`deviceClassMappings` on ClusterQueue ResourceGroups. If not configured,
Kueue admits DRA-backed workloads without device quota enforcement. There
is no operator-side validation that deviceClassMappings are configured.
Mitigation: documented in operations.md and troubleshooting.md.

---

## What Phase 9 should build next

### Immediate (Phase 9 Session 1)

1. **Wire observation gaps.** Complete G1, G2, G3 from the gaps analysis.
   This is ~30 lines of wiring code plus metric callsite additions.
   Estimated: 1 session.

2. **Update index.md API version note.** Mark the DRA API version boundary
   as resolved (G8).

### Near-term

3. **DRA + ProvisioningRequest interaction e2e.** Validate the composition
   of DRA device allocation with ProvisioningRequest node provisioning.

4. **Device failure recovery e2e.** When a DRA driver with fault injection
   is available, add end-to-end failure detection coverage.

### Medium-term (candidate Phase 9 scope)

5. **Real DRA driver integration.** Test with a real vendor DRA driver
   (e.g., NVIDIA DRA driver) in a CI environment with actual GPU
   allocation at the kubelet level.

6. **DRA-aware priority shaping.** Extend Phase 5 checkpoint-aware
   priority to account for device scarcity (e.g., higher protection
   priority for RTJs holding expensive accelerators).

7. **Shared ResourceClaims.** Pre-allocated devices reused across pods
   or runs. Useful for persistent device reservations.

8. **Multi-device-class alternatives.** Priority-ordered device class
   fallback (e.g., prefer A100, accept H100).

---

## Audit trail

| Document | Purpose |
|----------|---------|
| [review/consistency-audit.md](review/consistency-audit.md) | Phase 0-8 contract consistency verification |
| [review/gaps.md](review/gaps.md) | Gap analysis with severity classification |
| [session-handoff.md](session-handoff.md) | Sessions 1-10 decisions and file changes |

---

## Signoff

Phase 8 delivers a complete, contract-aligned DRA integration for RTJ.
All 9 core invariants are preserved. The design is conservative (fail-closed
checkpoint compatibility, native DRA only, no extended-resource bridge).
Test coverage meets minimum requirements across unit, e2e, and SDK layers.

The three P1 observation-wiring gaps are documented, non-blocking, and
scoped for Phase 9 Session 1 completion.

**Phase 8 is signed off.**
