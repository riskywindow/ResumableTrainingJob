# Phase 6 Open Questions

## OQ-1: MultiKueue External-Framework Protocol for Custom CRDs

**Question:** Does Kueue v0.15.1 MultiKueue support dispatching custom
external-framework CRDs (like RTJ) to worker clusters? What is the exact
protocol for remote object creation, spec propagation, and status
mirroring?

**Impact:** Blocks G1 (MultiKueue integration). If MultiKueue only
supports built-in job types (Job, JobSet), the RTJ operator would need
a custom dispatch adapter outside of MultiKueue.

**Resolution plan:** Inspect the Kueue v0.15.1 source for
`MultiKueueAdapter` interface, `MultiKueueJobReconciler`, and any
external-framework extension points. Check if the `GenericJob` interface
is sufficient or if additional registration is needed.

**Status:** Open.

---

## OQ-2: Pause Propagation via MultiKueue

**Question:** How does the manager propagate `spec.control.desiredState`
changes to the remote RTJ on the worker cluster? Does MultiKueue
propagate spec mutations on the manager-side object to the remote copy,
or does it only create the initial remote object?

**Impact:** Blocks G3 (shared-checkpoint remote pause/resume). If
MultiKueue does not propagate spec changes, the manager operator would
need direct cross-cluster API access to patch the remote RTJ.

**Resolution plan:** Inspect MultiKueue's reconciler to determine if it
watches for spec changes on the manager-side object and propagates them
to the remote copy. If not, evaluate alternatives:
- Manager operator directly patches the remote RTJ via kubeconfig.
- A sidecar controller on the manager watches for desiredState changes
  and patches the remote RTJ.

**Recommendation:** If MultiKueue does not propagate spec changes, the
simplest approach is for the manager operator to use the same kubeconfig
that MultiKueue uses (from MultiKueueCluster) to directly patch the
remote RTJ. This avoids adding a new transport mechanism.

**Status:** Open.

---

## OQ-3: Remote RTJ Status Visibility on the Manager

**Question:** How does MultiKueue make the remote RTJ's status visible
on the manager cluster? Does it mirror the full remote object status, or
only the Workload status?

**Impact:** Affects G4 (manager-visible remote status). The amount of
remote status available determines how much the manager can reflect
without direct cross-cluster API calls.

**Resolution plan:** Inspect MultiKueue's status mirroring logic. Check
if it mirrors the remote Workload status (sufficient for phase and
admission state) or the remote job object status (required for RTJ-
specific fields like lastCompletedCheckpoint, priorityShaping).

**Recommendation:** If MultiKueue only mirrors Workload status, the
manager operator can supplement by directly reading the remote RTJ
status using the MultiKueueCluster kubeconfig. This is a read-only
operation with no state mutation risk.

**Status:** Open.

---

## OQ-4: Manager-Mode Detection Without Runtime Cluster Inspection

**Question:** Should the manager-mode operator detect MultiKueue-managed
RTJs by checking the ClusterQueue's AdmissionCheck list, or should it
assume all RTJs are MultiKueue-managed when running in manager mode?

**Impact:** Affects G2 (manager/worker operator split). The detection
mechanism determines whether the manager mode is all-or-nothing or
per-RTJ.

**Resolution plan:** Evaluate two approaches:
1. **All-or-nothing (recommended for core milestone):** When the operator
   starts in manager mode, it assumes all RTJs are MultiKueue-managed.
   No local child JobSets are ever created. This is simpler and
   consistent with the hard boundary "do NOT make the manager cluster
   also act as a worker."
2. **Per-RTJ detection:** The operator inspects the Workload's
   AdmissionCheck list for a MultiKueue check. RTJs without MultiKueue
   are handled locally. This allows the manager to also serve as a
   worker for some RTJs.

**Recommendation:** All-or-nothing (approach 1). The hard boundary says
the manager does not act as a worker. Per-RTJ detection adds complexity
and blurs the manager/worker separation.

**Status:** Tentatively resolved (approach 1).

---

## OQ-5: Shared Checkpoint Store Credential Distribution

**Question:** How are credentials for the shared S3-compatible checkpoint
store distributed to all worker clusters?

**Impact:** Affects G3 and G5. Without proper credentials, workers cannot
access the shared store.

**Resolution plan:** This is an operational concern, not a code concern.
Document the requirement and provide guidance:
- For MinIO in local dev: same static credentials on all clusters.
- For AWS S3 in production: IAM roles for service accounts (IRSA) or
  cross-account access policies.
- For GCS: workload identity federation.

**Recommendation:** The operator does not manage credentials. It assumes
the `spec.checkpoint.storageURI` is accessible with the credentials
available in the pod's environment (env vars, mounted secrets, or IAM
roles). This is the same assumption as Phase 1-5.

**Status:** Tentatively resolved (operational concern, not code concern).

---

## OQ-6: MultiKueueCluster Kubeconfig Management in Kind

**Question:** How are kubeconfigs for the two worker kind clusters made
available to the manager cluster's Kueue MultiKueue controller?

**Impact:** Affects G5 (three-cluster dev/test profile). MultiKueue
needs kubeconfigs to access worker clusters.

**Resolution plan:** In kind, kubeconfigs are generated during cluster
creation. The dev profile script needs to:
1. Extract worker kubeconfigs from kind.
2. Create Secrets on the manager cluster containing the kubeconfigs.
3. Reference these Secrets from MultiKueueCluster resources.

**Recommendation:** Use `kind get kubeconfig --name worker-1` and
`kind get kubeconfig --name worker-2` to extract kubeconfigs. Modify
the server URLs to use the kind network's internal addresses (since the
manager cluster's Kueue controller runs inside Docker, not on the host).

**Status:** Open (requires kind networking investigation).

---

## OQ-7: Kueue MultiKueue Feature Gate Status in v0.15.1

**Question:** Is MultiKueue a stable/beta/alpha feature in Kueue v0.15.1?
Is it feature-gated? What feature gate name is used?

**Impact:** Affects the isolation strategy. If MultiKueue is alpha, the
Phase 6 path must be clearly documented as experimental and should not
affect the default Phase 5 path.

**Resolution plan:** Inspect Kueue v0.15.1 feature gates in the source.
Check the Kueue documentation for MultiKueue maturity level.

**Recommendation:** Regardless of maturity, Phase 6 keeps the MultiKueue
path isolated behind the `--mode=manager` startup flag. Worker mode
(default) is identical to Phase 5.

**Status:** Open.

---

## OQ-8: Remote RTJ Cleanup on Manager-Side Deletion

**Question:** When the user deletes the manager-side RTJ, does MultiKueue
clean up the remote RTJ on the worker cluster? Or does the manager
operator need to handle remote cleanup?

**Impact:** Affects lifecycle correctness. Orphaned remote RTJs would
continue running on the worker after the manager-side RTJ is deleted.

**Resolution plan:** Inspect MultiKueue's deletion behavior. If it
handles remote cleanup (via garbage collection or explicit deletion),
no operator action is needed. If not, the manager operator needs a
finalizer to clean up remote resources.

**Status:** Open.

---

## OQ-9: Interaction Between MultiKueue Dispatch and Kueue Preemption

**Question:** When Kueue preempts a MultiKueue-dispatched Workload on
the manager cluster, how does the preemption propagate to the worker?
Does MultiKueue handle the suspend/teardown on the remote cluster?

**Impact:** Affects the preemption path for multi-cluster RTJs. If
MultiKueue handles remote preemption, the worker executes the existing
graceful yield path. If not, the manager operator needs to trigger
remote preemption explicitly.

**Resolution plan:** Inspect MultiKueue's preemption handling. Check if
suspending the manager-side Workload causes MultiKueue to suspend the
remote Workload/RTJ.

**Recommendation:** If MultiKueue propagates suspension, this composes
cleanly with the existing Phase 2-5 graceful yield path on the worker.
No new preemption logic needed.

**Status:** Open.

---

## OQ-10: Phase 5 Deferred Items for Phase 6

**Question:** Should Phase 6 address the deferred items from Phase 5's
signoff (freshness-target RequeueAfter, CheckpointStoreError wiring,
PolicyRef immutability, StaleCheckpointBoost cleanup)?

**Impact:** These are minor improvements to the worker-side code. They
do not affect the multi-cluster architecture.

**Resolution plan:** Evaluate each item:
1. **Freshness-target RequeueAfter:** Worker-side only. Can be done in
   Phase 6 without affecting multi-cluster design.
2. **CheckpointStoreError wiring:** Worker-side only. Same.
3. **PolicyRef immutability:** Worker-side only. Same.
4. **StaleCheckpointBoost cleanup:** Worker-side only. Same.

**Recommendation:** Bundle these as minor worker-side improvements within
Phase 6 implementation sessions, separate from the core multi-cluster
work.

**Status:** Tentatively resolved (bundle with Phase 6 implementation).
