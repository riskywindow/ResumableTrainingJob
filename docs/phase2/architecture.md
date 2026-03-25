# Phase 2 Architecture

## Overview

Phase 2 moves Kueue up one layer.
In Phase 1, Kueue managed the child `JobSet`.
In Phase 2, Kueue manages the parent `RTJ` through external `jobframework` integration, while the child `JobSet` becomes a plain runtime resource created only after RTJ admission.

This gives Phase 2 the control-plane boundary we actually want:

- Kueue owns queueing, admission, and stock preemption decisions for RTJ.
- The RTJ controller owns graceful yield, checkpoint selection, and runtime launch or teardown.
- JobSet owns only runtime materialization.

## Control-Plane Boundary

| Concern | Kueue | RTJ Controller | Child JobSet |
| --- | --- | --- | --- |
| Queueing and admission | Authoritative | Observes and reacts | Not authoritative |
| Preemption decision | Authoritative | Observes and executes graceful yield | Not authoritative |
| Checkpoint selection | Not authoritative | Authoritative | Not authoritative |
| Runtime launch | Not authoritative | Authoritative | Materializes Pods |
| Runtime suspend and teardown | Not authoritative | Authoritative | Executes deletion and pod shutdown once ordered |

## Component Diagram

```mermaid
flowchart LR
    U[User or Platform Operator]
    K[Kueue]
    W[Workload managed for RTJ]
    R[ResumableTrainingJob]
    C[RTJ Controller]
    J[Plain Child JobSet]
    T[Trainer SDK or Agent]
    S[S3-Compatible Storage]

    U -->|create RTJ or patch manual intent| R
    K <--> |external integration via jobframework| R
    K --> W
    C -->|watch RTJ, Workload, checkpoints| R
    C -->|create or delete plain runtime| J
    J -->|run pods| T
    T -->|write or read DCP checkpoints| S
    T -->|yield or restore signals| C
```

## Create -> Queue -> Admit -> Launch

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant R as RTJ
    participant K as Kueue
    participant W as Workload
    participant C as RTJ Controller
    participant J as Child JobSet
    participant T as Trainer

    U->>R: Create RTJ with queue target and desired run intent
    K->>R: Default RTJ into suspended state through webhook rules
    K->>W: Create Workload from RTJ pod sets via jobframework
    W-->>K: Remains pending in queue
    C->>R: Publish phase=Queued
    K-->>W: Admit Workload
    K-->>R: Clear suspension or mark RTJ admitted
    C->>R: Publish phase=Admitted
    C->>C: Render JobSet from RTJ template plus admitted pod-set info
    C->>J: Create plain child JobSet attempt 1
    J->>T: Start training pods
    C->>R: Publish phase=Starting then phase=Running
```

## Kueue-Driven Preemption -> Graceful Yield -> Checkpoint -> Teardown

```mermaid
sequenceDiagram
    autonumber
    participant K as Kueue
    participant R as RTJ
    participant C as RTJ Controller
    participant J as Child JobSet
    participant T as Trainer
    participant S as Storage
    participant W as Workload

    K-->>R: Suspend or preempt admitted RTJ
    C->>R: Publish phase=YieldRequested
    C-->>T: Send graceful yield request for current attempt
    T-->>C: Ack yield request
    C->>R: Publish phase=Draining
    T->>T: Wait for next step boundary
    T->>S: Write DCP checkpoint and publish manifest last
    T-->>C: Report completed checkpoint metadata
    C->>S: Revalidate latest compatible complete checkpoint
    C->>J: Delete plain child JobSet
    J-->>C: Runtime removed
    C->>R: Clear active runtime state
    C->>R: Publish phase=Queued if desiredState=Running
    W-->>K: RTJ remains queued until re-admission
```

The post-yield steady state depends on intent source:

- if the user requested `Paused`, the steady state is `Paused`
- if Kueue suspended a still-runnable RTJ, the steady state is `Queued`

The drain path is the same in both cases.
The steady state differs because manual pause is sticky user intent, while Kueue preemption is queueing intent.

## Re-Admission -> Resume

```mermaid
sequenceDiagram
    autonumber
    participant K as Kueue
    participant W as Workload
    participant R as RTJ
    participant C as RTJ Controller
    participant S as Storage
    participant J as Child JobSet
    participant T as Trainer

    K-->>W: Re-admit queued RTJ Workload
    K-->>R: Unsuspend or mark RTJ admitted
    C->>S: Select latest compatible complete checkpoint
    C->>R: Publish phase=Restoring with selectedCheckpoint
    C->>C: Render JobSet from RTJ template plus admitted pod-set info
    C->>J: Create plain child JobSet for new attempt
    J->>T: Start restore attempt
    T->>S: Read selected checkpoint
    T->>T: Restore DCP state and resume loop
    T-->>C: Report restore success
    C->>R: Publish phase=Running
```

## Key Design Notes

- The RTJ `GenericJob` implementation must derive Kueue pod sets from the embedded JobSet template without creating the child JobSet early.
- The RTJ controller must stamp admitted pod-set decisions back onto the rendered child JobSet so the runtime honors Kueue admission.
- Queue and priority identity belong on RTJ, not on the child JobSet.
- The child JobSet must not be visible to Kueue as a second workload.
- Phase 2 keeps the Phase 1 checkpoint contract, object-store contract, and run-attempt model.
- If the cluster uses Kueue features that auto-manage unlabeled workloads, Phase 2 must explicitly prevent the child JobSet from being picked up; otherwise the single-admission-object boundary breaks.
