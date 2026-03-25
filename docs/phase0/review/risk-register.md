# Initial Risk Register

This register captures the main design risks visible in Phase 0.
Each item SHOULD be revisited before a concrete API or controller design is accepted.

| ID | Risk | Why It Matters | Proposed Phase 0 Handling |
| --- | --- | --- | --- |
| R1 | The RTJ shape may be mistaken for a finalized production CRD too early. | Premature transport lock-in would constrain later API and lifecycle choices before implementation planning is complete. | Keep the RTJ schema explicitly conceptual and record that a later ADR is still required before a real CRD is standardized. |
| R2 | Checkpoint completeness or integrity checks may still be implemented inconsistently. | A controller cannot make a defensible yield or resume decision if manifest-last completion and artifact verification are applied unevenly. | Define manifest-last completion, minimum manifest fields, integrity metadata, and corruption handling in the checkpoint contract pack and carry them into implementation planning. |
| R3 | Manual yield and Kueue-driven yield may diverge in behavior. | Divergent control paths would make policy, status, and operator expectations inconsistent. | Keep one RTJ lifecycle and one status model regardless of yield source, and preserve the shared invariants in the contracts. |
| R4 | Users may assume broader resume portability than `v1` actually supports. | Misaligned expectations would produce unsafe or disputed resume attempts across clusters, images, or shapes. | State exact same-cluster, same-image, same-code-version, same-world-size, and same-GPU-shape rules in the API and status contracts. |
| R5 | Embedded runtime-template details may drift beyond the narrow JobSet-only scope. | Over-specifying nested runtime fields in Phase 0 would blur the line between conceptual contract work and implementation design. | Keep nested runtime templates conceptual, require JobSet compatibility only, and defer full nested schema standardization. |
