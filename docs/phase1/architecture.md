# Phase 1 Architecture

## Overview

Phase 1 keeps the Phase 0 authority model intact while narrowing implementation to one manual pause and resume path.
The operator owns RTJ lifecycle coordination.
Kueue owns queueing and admission for the child `JobSet`.
The Python trainer owns in-process checkpoint and restore work through PyTorch DCP.

Phase 1 intentionally does not implement Kueue-driven preemption yet.
Manual control uses `spec.control.desiredState`, which already exists in the accepted Phase 0 API contract, so no Phase 1 API extension is required for pause and resume.

Checkpointing is synchronous:

1. write checkpoint data to a local filesystem staging directory inside the Pod
2. upload staged artifacts to S3-compatible storage
3. publish the checkpoint manifest last
4. exit the running attempt only after upload and manifest publication succeed

## Component Diagram

```mermaid
flowchart LR
    U[User or Operator]
    RTJ[ResumableTrainingJob CR]
    O[RTJ Operator<br/>Go]
    K[Kueue]
    J[Child JobSet]
    T[Toy Trainer<br/>Python + DCP]
    S[S3-Compatible Storage]

    U -->|create or patch desiredState| RTJ
    O -->|watch| RTJ
    O -->|create/update/delete child JobSet| J
    J <-->|queueing and admission| K
    J -->|run worker Pods| T
    T -->|sync checkpoint upload| S
    T -->|restore selected checkpoint| S
    O -->|publish phase and checkpoint refs| RTJ
```

## Launch Sequence

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant O as RTJ Operator
    participant R as RTJ
    participant J as JobSet
    participant K as Kueue
    participant T as Toy Trainer
    participant S as Object Storage

    U->>R: Create RTJ with desiredState=Running
    O->>R: Validate spec and initialize status
    O->>J: Create child JobSet attempt 1
    J->>K: Register through built-in JobSet integration
    K-->>J: Admit JobSet
    J->>T: Start trainer Pods
    T->>S: Optionally read latest checkpoint metadata
    T-->>O: Report running state
    O->>R: Publish phase=Running
```

## Manual Pause Sequence

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant R as RTJ
    participant O as RTJ Operator
    participant J as JobSet
    participant T as Toy Trainer
    participant S as Object Storage

    U->>R: Patch spec.control.desiredState=Paused
    O->>R: Publish phase=YieldRequested
    O-->>T: Signal pause intent for active attempt
    T->>T: Wait for next training step boundary
    T->>T: Write DCP checkpoint to local staging directory
    T->>S: Upload staged checkpoint artifacts
    T->>S: Write manifest last
    T-->>O: Exit attempt after successful upload
    O->>S: Validate latest compatible complete checkpoint
    O->>J: Remove or finalize child JobSet attempt 1
    O->>R: Publish phase=Paused and checkpoint refs
```

## Resume Sequence

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant R as RTJ
    participant O as RTJ Operator
    participant J as JobSet
    participant K as Kueue
    participant T as Toy Trainer
    participant S as Object Storage

    U->>R: Patch spec.control.desiredState=Running
    O->>S: Select latest compatible complete checkpoint
    O->>R: Publish phase=Restoring and selectedCheckpoint
    O->>J: Create child JobSet attempt 2 with checkpoint reference
    J->>K: Register through built-in JobSet integration
    K-->>J: Admit JobSet
    J->>T: Start trainer Pods with restore config
    T->>S: Read selected checkpoint
    T->>T: Restore DCP state and resume loop
    T-->>O: Report running state
    O->>R: Publish phase=Running
```

## Design Notes

- The child runtime stays a `JobSet` so Kueue can manage it through built-in integration. Phase 1 does not add native Kueue custom-job support for RTJ itself.
- `spec.control.desiredState` is the only manual control surface for the slice. A later phase may add a subresource or other transport, but Phase 1 does not need a new API field.
- The default fixture should use CPU plus `gloo` so the entire path runs inside `kind` without GPU dependencies.
- Object storage should be local and disposable in development, but the checkpoint layout and manifest-last semantics must still match the accepted Phase 0 contracts.
- The operator must preserve the single-active-runtime invariant even during pause and resume transitions.
