# System Context

This document describes the concrete `v1` system context for the `checkpoint-native preemption controller`.
It is intended to guide implementation planning by showing the main components, their interactions, and the source of authority for core decisions.

## Component Diagram

```mermaid
flowchart LR
    U[Platform Engineer or Operator]
    K[Kueue]
    O[ResumableTrainingJob Operator]
    J[JobSet]
    subgraph P[Training Pods]
        A[Training SDK or Agent]
        C[User Training Code]
    end
    S[S3-Compatible Object Storage]

    U -->|submit job or request manual yield| O
    O <--> |queueing, admission, preemption intent| K
    O -->|create or update runtime workload| J
    J -->|create worker Pods| A
    A <--> C
    A -->|write DCP checkpoint| S
    A -->|checkpoint status and metadata| O
    O -->|resume with selected checkpoint| J
    A -->|read selected checkpoint| S
```

## Main Happy Path: Submission to Resume

```mermaid
sequenceDiagram
    autonumber
    actor U as Platform Engineer
    participant O as ResumableTrainingJob Operator
    participant K as Kueue
    participant J as JobSet
    participant A as Training SDK/Agent
    participant C as User Training Code
    participant S as S3 Object Storage

    U->>O: Submit ResumableTrainingJob
    O->>K: Register workload for queueing and admission
    K-->>O: Admit workload
    O->>J: Create admitted JobSet
    J->>A: Start worker Pods with SDK/agent
    A->>C: Launch training
    C-->>A: Reach training step boundaries during execution
    K-->>O: Signal preemption intent
    O-->>A: Request graceful yield
    A->>C: Wait for next safe training step boundary
    C-->>A: Safe point reached
    A->>S: Write DCP checkpoint
    S-->>A: Persist checkpoint objects
    A-->>O: Publish checkpoint metadata and completion
    O->>J: Mark yield complete and stop current runtime
    U->>O: Request resume or allow queued resume
    O->>K: Request admission for resume
    K-->>O: Admit resume
    O->>O: Select latest compatible checkpoint
    O->>J: Create resumed JobSet with checkpoint reference
    J->>A: Start resumed worker Pods
    A->>S: Read selected checkpoint
    A->>C: Restore and continue training
```

## Authority Matrix

Legend:

- `A`: authoritative
- `S`: supporting but not authoritative
- `N`: not authoritative

| Concern | Kueue | ResumableTrainingJob Operator | JobSet | Training SDK/Agent | Object Storage | User Training Code | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Admission | A | S | N | N | N | N | Kueue decides whether the workload may run. |
| Preemption intent | A | A | N | N | N | N | Kueue is authoritative for queue-driven intent; the operator is authoritative for manual yield intake and normalization. |
| Safe points | N | N | N | S | N | A | User code defines valid training step boundaries; the SDK/agent exposes them to the product flow. |
| Checkpoint writes | N | N | N | A | S | S | The SDK/agent performs DCP writes; object storage only persists bytes. |
| Checkpoint selection | N | A | N | N | N | N | The operator chooses the checkpoint used for resume under ADR 0003. |
| Restore orchestration | N | A | S | S | N | N | The operator initiates resume, JobSet carries the workload, and the SDK/agent performs in-process restore steps. |
| Runtime execution | N | N | S | S | N | A | User code owns training behavior; JobSet and the SDK/agent only host and mediate execution. |

## Implementation-Guiding Notes

- The operator MUST treat Kueue as the source of truth for admission and queue-driven preemption.
- The operator MUST NOT treat object presence in S3 as sufficient evidence of a compatible checkpoint without the metadata checks from ADR 0003.
- The SDK or agent MUST be the only component that writes DCP checkpoints.
- User training code MUST remain the source of truth for training-step semantics and safe-point placement.
- JobSet SHOULD remain a workload carrier and reconciler, not a place to embed product policy.
