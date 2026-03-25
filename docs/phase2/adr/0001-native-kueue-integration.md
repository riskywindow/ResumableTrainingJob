# ADR 0001: Native Kueue Integration For RTJ

- Status: Accepted
- Date: 2026-03-22

## Context

Phase 1 proved the manual checkpoint, pause, and resume path, but it kept Kueue attached to the child `JobSet`.
That created a structural mismatch with the product boundary:

- RTJ was the product-level lifecycle object
- the child `JobSet` was the Kueue-managed admission object
- Kueue-driven preemption for RTJ itself was still deferred

Phase 2 needs one authoritative admission object for each resumable training lineage.
That admission object must be RTJ, not the child `JobSet`.

This also needs to follow Kueue's current supported external integration path via `jobframework`.

## Decision

### 1. RTJ Becomes The Only Kueue-Managed Admission Object

Phase 2 will integrate RTJ with Kueue as an external framework using `jobframework`.
Kueue will manage queueing, admission, and stock preemption for RTJ itself.

The RTJ CRD must gain a suspend-like field in `spec` so the Kueue webhook and reconciler can manage admission state using the supported external integration model.

### 2. Child JobSet Becomes Runtime-Only

The child `JobSet` remains the runtime primitive, but it is no longer a Kueue-managed workload.

That means:

- no Kueue queue label on the child `JobSet`
- no Kueue workload-priority label on the child `JobSet`
- no design that expects Kueue to admit or preempt the child `JobSet`

The child `JobSet` is created only after RTJ admission and removed after yield or terminal completion.

### 3. RTJ Pod Accounting Comes From The Embedded JobSet Template

The RTJ `GenericJob` implementation must derive the Kueue pod-set and resource footprint from the embedded JobSet template already carried by RTJ.
Phase 2 does not introduce a second runtime template format.

Admission data produced for the RTJ-owned `Workload` must be projected back into the rendered child `JobSet` so the launched runtime obeys Kueue's decision.

### 4. Kueue-Driven Suspend And Manual Pause Share One Drain Path

Phase 2 brings Kueue-driven suspend and preemption into scope.
Both Kueue-driven suspend and manual pause must converge on the same controlled runtime path:

1. accept or observe suspend intent
2. publish yield request to the runtime
3. wait for a step-boundary checkpoint
4. validate the latest compatible complete checkpoint
5. tear down the child `JobSet`

Phase 2 still does not add a custom preemption algorithm.
It only makes RTJ respond correctly to stock Kueue suspension and preemption behavior.

### 5. Exact Contract Between Manual Pause And Kueue Suspend

The tension is that Kueue needs a suspend-like field in `RTJ.spec`, while Phase 1 already exposed manual control through `spec.control.desiredState`.

Phase 2 resolves that tension with this contract:

- the new suspend-like field is the Kueue-facing admission gate
- `spec.control.desiredState` remains optional compatibility input if practical
- `desiredState=Running` means the user allows the RTJ to run when Kueue admits it; it does not bypass queueing or force admission
- `desiredState=Paused` means the user requests a sticky manual pause using the same graceful drain path as Kueue suspend
- if Kueue suspends an RTJ while `desiredState=Running`, the RTJ should checkpoint, tear down the runtime, and return to `Queued`
- if the user sets `desiredState=Paused`, the RTJ should checkpoint, tear down the runtime, and remain `Paused` until the user changes intent

So Phase 2 preserves one drain path, but it distinguishes the post-drain steady state:

- Kueue suspend is queueing intent
- manual pause is sticky user intent

Manual pause must never override Kueue's authority for queue admission.
It only decides whether the RTJ is eligible to relaunch after the drain completes.

### 6. Pinned Versions Stay In Place

Phase 2 should continue with the versions already pinned in the repo unless a concrete blocker appears:

- Kueue `v0.15.1`
- JobSet `v0.10.1`

## Consequences

Positive consequences:

- Phase 2 matches the intended product boundary: one product object, one admission object.
- RTJ can surface real `Queued` and `Admitted` lifecycle phases.
- Kueue-driven preemption becomes a native RTJ lifecycle event instead of a Phase 1 gap.
- JobSet goes back to being a runtime carrier instead of a split policy object.

Negative consequences:

- Phase 2 needs RTJ API changes for a suspend-like field and webhook behavior.
- The controller must project admitted pod-set information into the runtime `JobSet`.
- Existing Phase 1 scripts and examples that patch only `desiredState` will need careful compatibility handling.
- Clusters that auto-manage unlabeled workloads need an explicit child-JobSet opt-out plan.

## Rejected Alternatives

### Keep The Child JobSet As The Kueue-Managed Object

Rejected because it preserves the Phase 1 split-brain control plane and prevents RTJ from becoming the native admission object.

### Let Both RTJ And Child JobSet Be Kueue-Managed

Rejected because it creates two admission objects for one training lineage and breaks clean ownership of queueing and preemption.

### Add A Custom Scheduler Or Custom Preemption Algorithm

Rejected because the Phase 0 and Phase 2 boundaries explicitly keep scheduling and preemption policy inside stock Kueue behavior.
