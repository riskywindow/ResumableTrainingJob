# Design Review Checklist

Use this checklist before accepting any Phase 0 artifact as the baseline for implementation planning.

## Scope Gate

- Does the design stay within the narrow `v1` boundary from ADR 0001?
- Does every artifact describe a single-RTJ graceful-yield and same-identity-resume flow rather than a general orchestration system?
- Are runtime-specific checkpoint write and restore mechanics explicitly treated as SDK or agent responsibilities rather than controller code?
- Are migration, placement expansion, elastic resize, and batching clearly excluded from `v1`?

## Contract Gate

- If the contract is RTJ-shaped, is it clearly marked as a conceptual Phase 0 artifact rather than a finalized production CRD?
- Does the RTJ require enough declared identity to enforce the accepted resume-compatibility contract?
- Does the RTJ require an explicit bounded checkpoint cadence, freshness budget, and max drain time?
- Does the contract keep checkpoint selection fixed to the latest compatible complete checkpoint in `v1`?
- Does the checkpoint contract require manifest-last completion and enough manifest metadata to prove completeness and compatibility?

## Failure Semantics Gate

- Is the behavior clear when a workload cannot reach a safe training step boundary before `maxDrainTime` expires?
- Is the behavior clear when the active runtime disappears or loses admission during yield or resume?
- Is the behavior clear when the checkpoint artifact set is partial, stale, or incompatible?
- Is the fallback behavior clear when the newest checkpoint fails restore validation or is found corrupt?
- Is the behavior clear when resume retries are exhausted?

## Security and Policy Gate

- Is the actor allowed to create or mutate the RTJ and request manual yield through the chosen control surface?
- Is access to checkpoint artifacts expected to follow existing storage and workload policies?
- Are tenant boundaries preserved by keeping RTJs and their referenced templates scoped to the correct namespace or policy boundary?

## Operability Gate

- Can an operator determine why an RTJ is `Queued`, `Admitted`, `Starting`, `YieldRequested`, `Draining`, `Paused`, `Restoring`, or `Failed`?
- Is there a bounded status vocabulary suitable for alerting and dashboards?
- Are checkpoint interval, freshness budget, max drain time, and retry fields bounded tightly enough for a first release?

## Exit Criteria

Phase 0 SHOULD NOT advance to implementation planning until each checklist item is either accepted or tracked as an explicit open question in `session-handoff.md`.
