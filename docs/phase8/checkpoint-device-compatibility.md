# Checkpoint Device Profile Compatibility

Phase 8 extends checkpoint compatibility checking with a **device profile
fingerprint** dimension. When DRA is enabled, checkpoints record the SHA256
fingerprint of the device profile under which they were saved. Resume
selection enforces a **fail-closed** compatibility rule: a checkpoint is
only eligible for resume when the device profiles match exactly.

## Design Principles

1. **Conservative compatibility**: No attempt to infer semantic
   cross-device compatibility from device class names or CEL selectors.
   Only exact fingerprint match is safe.

2. **Fail-closed**: When the current RTJ has a device profile and the
   checkpoint does not, the checkpoint is rejected. This prevents
   resuming a non-DRA checkpoint under a DRA-configured runtime, which
   could lead to hardware mismatch.

3. **Backward compatible**: When neither the request nor the checkpoint
   has a device profile (Phase 7 and earlier), the device dimension
   check is skipped entirely. All Phase 7 compatibility behavior is
   preserved unchanged.

4. **No vendor introspection**: Compatibility is determined solely from
   the device profile fingerprint (SHA256 of canonical sorted device
   class names, CEL selectors, and counts). No hardware-specific
   knowledge is required.

## Device Profile Fingerprint

The fingerprint is computed by `internal/dra/profile.go:BuildProfile()`:

1. Each claim contributes a canonical entry:
   `class=<className>;selectors=<sorted,joined>;count=<count>`
2. Entries are sorted alphabetically for order independence.
3. Entries are joined with newlines.
4. SHA256 of the canonical string produces the fingerprint.

Container targets do not affect the fingerprint (they are a rendering
concern, not a hardware requirement).

## Compatibility Rules

### Dimension 12: Device Profile Fingerprint (Phase 8)

| Checkpoint Fingerprint | Request Fingerprint | Result |
|---|---|---|
| Empty | Empty | Compatible (Phase 7 behavior) |
| Non-empty | Empty | Compatible (DRA to non-DRA downgrade) |
| Empty | Non-empty | **Incompatible** (non-DRA checkpoint under DRA) |
| Same hash | Same hash | Compatible |
| Different hash | Different hash | **Incompatible** (device profile mismatch) |

### Interaction with Other Dimensions

The device profile check runs **after** all existing compatibility
dimensions (cluster, lineage, runtime, world size, GPU shape, image,
code version, format, optimizer, sharding). All existing checks must
pass before the device profile check is evaluated.

The device profile check is independent of world-size flexibility:
a checkpoint with `AllowWorldSizeChange=true` and
`CrossSizeRestoreSupported=true` can still be rejected if the device
profiles don't match.

## Manifest Extension

The `CheckpointManifest` struct (Go) and `CheckpointManifest` dataclass
(Python SDK) are extended with an optional `deviceProfileFingerprint`
field:

### Go (`internal/checkpoints/types.go`)

```go
type CheckpointManifest struct {
    // ... existing fields ...
    DeviceProfileFingerprint string `json:"deviceProfileFingerprint,omitempty"`
}
```

### Python SDK (`sdk/python/yield_sdk/manifest.py`)

```python
@dataclass
class CheckpointManifest:
    # ... existing fields ...
    device_profile_fingerprint: str | None = None
```

The field is optional with `omitempty` / `None` default. Phase 7 and
earlier manifests decode without error (the field defaults to empty/None).

## Resume Request Extension

The `ResumeRequest` struct is extended with:

```go
type ResumeRequest struct {
    // ... existing fields ...
    CurrentDeviceProfileFingerprint string
}
```

When DRA is enabled, `ResumeRequestFromRTJ()` and the controller's
`resumeCheckpointForAttempt()` populate this field from
`status.devices.currentDeviceProfileFingerprint`.

## Status Tracking

The RTJ `status.devices` section tracks two device profile fingerprint
history fields:

- `lastCheckpointDeviceProfileFingerprint`: the fingerprint from the
  most recently completed checkpoint manifest.
- `lastResumeDeviceProfileFingerprint`: the fingerprint that was active
  when the most recent resume was performed.

These are informational and used for debugging/observability, not for
compatibility decisions (which always use the current profile and the
manifest's recorded profile).

## Claim Allocation Observation

Phase 8 Session 5 also adds DRA claim allocation state observation:

### Status Fields

- `status.devices.claimAllocationState`: Pending | Allocated | Failed | Unknown
- `status.devices.allocatedClaimCount`: number of successfully allocated claims
- `status.devices.lastClaimFailureReason`: reason string from the last failure
- `status.devices.lastClaimFailureTime`: timestamp of the last failure

### Observation Logic

1. The controller lists all ResourceClaims in the namespace.
2. Claims are filtered to those matching the RTJ (by label or template annotation).
3. Each claim is classified:
   - **Allocated**: `claim.Status.Allocation != nil` (without device-level failures)
   - **Failed**: allocated but with per-device conditions indicating failure
     (`Ready=False`, `AllocationFailed`, `DriverError`, etc.)
   - **Pending**: not yet allocated
4. Aggregate state:
   - Any failed → `Failed`
   - All allocated → `Allocated`
   - Otherwise → `Pending`

### Condition

When claim allocation fails, a `DRAClaimAllocationFailed` condition is
set on the RTJ. It is cleared when all claims are successfully allocated.
