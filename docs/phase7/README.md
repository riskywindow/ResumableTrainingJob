# Phase 7 -- Quick Start

Phase 7 adds **capacity-guaranteed launch** to ResumableTrainingJob.

Today (Phase 6) an RTJ launches its child JobSet as soon as Kueue admits the
workload -- meaning quota is reserved. But quota reservation does not mean
physical nodes are ready. If a cluster-autoscaler must scale up, pods sit
Pending for minutes or longer. Startup timeout failures become ambiguous:
is it a bug in the training image, or are nodes still scaling?

Phase 7 closes this gap:

1. **ProvisioningRequest AdmissionCheck** -- Kueue's built-in mechanism to
   gate admission on a ProvisioningRequest resource that models physical
   capacity. RTJ does not invent a second gate; it configures Kueue's
   existing one.

2. **Provisioning-aware launch gating** -- the RTJ operator will not render
   the child JobSet until _all_ AdmissionChecks (including the provisioning
   one) report success. This means child pods only appear when nodes are
   physically available.

3. **Topology-second-pass rendering** -- topology assignment may arrive
   _after_ provisioning completes (a second Kueue reconciliation pass).
   The launch gate waits for both provisioning success and topology
   assignment before rendering.

4. **waitForPodsReady startup/recovery integration** -- Kueue's
   waitForPodsReady feature gives us coherent startup and recovery
   timeouts. If pods don't reach Ready within the configured window,
   Kueue evicts the workload. RTJ surfaces these timeouts in its status.

5. **Fake ProvisioningRequest backend** -- a lightweight controller for
   local dev and e2e tests that auto-approves ProvisioningRequests after
   a configurable delay, so the full flow is testable without a real
   cluster-autoscaler.

6. **Optional real cloud profile** -- the real-cloud path (GKE NAP,
   Karpenter, etc.) is documented but not required for local success.

## What stays the same

- RTJ is the only Kueue-managed object.
- Child JobSets are plain runtime resources.
- Kueue remains the admission and preemption authority.
- All Phase 1-6 features work unchanged when Phase 7 is not configured.
- The RTJ lifecycle state machine is unchanged.

## See also

- [index.md](index.md) -- full document index
- [goals.md](goals.md) -- acceptance criteria
- [architecture.md](architecture.md) -- diagrams and design
