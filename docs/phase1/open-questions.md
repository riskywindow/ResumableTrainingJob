# Phase 1 Open Questions

These are the remaining implementation-shaped questions after the current Phase 1 slice was built and signed off.
They do not reopen the accepted Phase 0 scope.

## Runtime Evidence For `Running`

Phase 1 uses a mounted JSON control file and storage polling for the pause and resume happy path.
The next unresolved question is what the smallest bounded runtime signal should be for:

- restore completion
- running heartbeat
- stale-runtime detection

## Queue And Admission Visibility

The API carries `Queued` and `Admitted`, but the controller does not yet publish them.
The open question is how much Kueue state Phase 2 should surface in RTJ status without overfitting to one queue implementation detail.

## Resume Failure Diagnostics

The catalog and trainer can reject many resume candidates or restore attempts, but RTJ status does not yet explain those decisions in detail.
The open question is whether the next step should use:

- Kubernetes events
- bounded status history
- both

## Repeated-Cycle Test Shape

Phase 1 has one pause smoke and one resume smoke.
The remaining question is what the smallest non-flaky repeated pause and resume test should look like once the runtime evidence path is stronger.
