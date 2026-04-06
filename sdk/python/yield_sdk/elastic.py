"""Phase 9: Runtime-side elasticity protocol for cooperative resize.

This module implements the narrow control protocol for manual target-based
resize.  The runtime can observe current vs target worker counts, determine
whether a shrink can be handled in-place, and produce a clear success/fallback
signal for the controller.

Protocol summary
~~~~~~~~~~~~~~~~
1. The operator writes ``targetWorkerCount`` into the control file or sets
   ``YIELD_SDK_TARGET_WORKER_COUNT`` at launch.
2. The runtime reads the elastic config and calls ``evaluate_resize()``.
3. The outcome tells the controller which path to take:
   - **Grow**: always checkpoint-and-relaunch.
   - **Shrink + in-place supported**: cooperative rank exit (signal SUCCESS).
   - **Shrink + in-place NOT supported**: checkpoint-and-relaunch fallback.
4. The runtime writes a resize signal file so the controller can observe the
   outcome deterministically.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from enum import Enum
from pathlib import Path
from typing import Any


# ---------------------------------------------------------------------------
# Enums
# ---------------------------------------------------------------------------

class ElasticityMode(str, Enum):
    """Maps to ``spec.elasticity.mode`` on the RTJ CRD."""
    DISABLED = "Disabled"
    MANUAL = "Manual"


class ResizeDirection(str, Enum):
    """Direction of a resize request."""
    NONE = "None"
    SHRINK = "Shrink"
    GROW = "Grow"


class ShrinkOutcome(str, Enum):
    """Outcome of a shrink evaluation."""
    SUCCESS = "Success"
    FALLBACK_REQUIRED = "FallbackRequired"
    NOT_REQUESTED = "NotRequested"


# ---------------------------------------------------------------------------
# Elastic config (read-only view of operator intent)
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class ElasticConfig:
    """Runtime-visible elasticity configuration.

    Populated from ``YIELD_SDK_*`` environment variables or from the control
    file's ``targetWorkerCount`` field.  The ``current_worker_count`` is the
    live ``WORLD_SIZE`` observed by the distributed runtime.
    """

    mode: ElasticityMode = ElasticityMode.DISABLED
    current_worker_count: int = 0
    target_worker_count: int = 0
    supports_in_place_shrink: bool = False
    shrink_barrier_timeout_seconds: float = 30.0

    @property
    def resize_direction(self) -> ResizeDirection:
        if self.mode == ElasticityMode.DISABLED:
            return ResizeDirection.NONE
        if self.target_worker_count == self.current_worker_count:
            return ResizeDirection.NONE
        if self.target_worker_count < self.current_worker_count:
            return ResizeDirection.SHRINK
        return ResizeDirection.GROW

    @property
    def resize_requested(self) -> bool:
        return self.resize_direction != ResizeDirection.NONE

    @property
    def worker_delta(self) -> int:
        """Positive means grow, negative means shrink, zero means no change."""
        return self.target_worker_count - self.current_worker_count

    @classmethod
    def from_env(cls, *, current_worker_count: int | None = None) -> "ElasticConfig":
        """Build from ``YIELD_SDK_*`` environment variables.

        Parameters
        ----------
        current_worker_count:
            Override for the live world size.  When *None* the value is read
            from ``YIELD_SDK_WORLD_SIZE`` / ``WORLD_SIZE`` (falling back to 1).
        """
        mode_raw = os.environ.get("YIELD_SDK_ELASTICITY_MODE", "Disabled")
        try:
            mode = ElasticityMode(mode_raw)
        except ValueError:
            mode = ElasticityMode.DISABLED

        if current_worker_count is None:
            current_worker_count = int(
                os.environ.get("YIELD_SDK_WORLD_SIZE", os.environ.get("WORLD_SIZE", "1"))
            )

        target_raw = os.environ.get("YIELD_SDK_TARGET_WORKER_COUNT", "")
        target_worker_count = int(target_raw) if target_raw else current_worker_count

        supports_in_place = os.environ.get(
            "YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK", ""
        ).lower() in ("1", "true", "yes")

        barrier_timeout = float(
            os.environ.get("YIELD_SDK_SHRINK_BARRIER_TIMEOUT", "30.0")
        )

        return cls(
            mode=mode,
            current_worker_count=current_worker_count,
            target_worker_count=target_worker_count,
            supports_in_place_shrink=supports_in_place,
            shrink_barrier_timeout_seconds=barrier_timeout,
        )


# ---------------------------------------------------------------------------
# Resize outcome
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class ResizeOutcome:
    """Result of evaluating a resize request at the runtime level.

    The controller reads this (via the signal file) to decide whether to
    proceed with in-place shrink or fall back to checkpoint-and-relaunch.
    """

    direction: ResizeDirection
    outcome: ShrinkOutcome
    current_worker_count: int
    target_worker_count: int
    in_place_shrink_supported: bool
    requires_checkpoint: bool
    fallback_reason: str | None = None

    def to_dict(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "direction": self.direction.value,
            "outcome": self.outcome.value,
            "currentWorkerCount": self.current_worker_count,
            "targetWorkerCount": self.target_worker_count,
            "inPlaceShrinkSupported": self.in_place_shrink_supported,
            "requiresCheckpoint": self.requires_checkpoint,
        }
        if self.fallback_reason:
            payload["fallbackReason"] = self.fallback_reason
        return payload

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "ResizeOutcome":
        return cls(
            direction=ResizeDirection(payload["direction"]),
            outcome=ShrinkOutcome(payload["outcome"]),
            current_worker_count=int(payload["currentWorkerCount"]),
            target_worker_count=int(payload["targetWorkerCount"]),
            in_place_shrink_supported=bool(payload["inPlaceShrinkSupported"]),
            requires_checkpoint=bool(payload["requiresCheckpoint"]),
            fallback_reason=payload.get("fallbackReason"),
        )


def evaluate_resize(config: ElasticConfig) -> ResizeOutcome:
    """Evaluate a resize request and return a deterministic outcome.

    Decision tree::

        direction == NONE  -> NOT_REQUESTED, no checkpoint
        direction == GROW  -> NOT_REQUESTED (not a shrink), checkpoint required
        direction == SHRINK:
            supports_in_place_shrink -> SUCCESS, no checkpoint
            else                     -> FALLBACK_REQUIRED, checkpoint required
    """
    direction = config.resize_direction

    if direction == ResizeDirection.NONE:
        return ResizeOutcome(
            direction=ResizeDirection.NONE,
            outcome=ShrinkOutcome.NOT_REQUESTED,
            current_worker_count=config.current_worker_count,
            target_worker_count=config.target_worker_count,
            in_place_shrink_supported=config.supports_in_place_shrink,
            requires_checkpoint=False,
        )

    if direction == ResizeDirection.GROW:
        return ResizeOutcome(
            direction=ResizeDirection.GROW,
            outcome=ShrinkOutcome.NOT_REQUESTED,
            current_worker_count=config.current_worker_count,
            target_worker_count=config.target_worker_count,
            in_place_shrink_supported=config.supports_in_place_shrink,
            requires_checkpoint=True,
            fallback_reason="grow always requires checkpoint-and-relaunch",
        )

    # Shrink path
    if config.supports_in_place_shrink:
        return ResizeOutcome(
            direction=ResizeDirection.SHRINK,
            outcome=ShrinkOutcome.SUCCESS,
            current_worker_count=config.current_worker_count,
            target_worker_count=config.target_worker_count,
            in_place_shrink_supported=True,
            requires_checkpoint=False,
        )

    return ResizeOutcome(
        direction=ResizeDirection.SHRINK,
        outcome=ShrinkOutcome.FALLBACK_REQUIRED,
        current_worker_count=config.current_worker_count,
        target_worker_count=config.target_worker_count,
        in_place_shrink_supported=False,
        requires_checkpoint=True,
        fallback_reason=(
            "runtime does not support in-place shrink"
            " (DDP requires process group reinitialization)"
        ),
    )


# ---------------------------------------------------------------------------
# Resize checkpoint context (metadata for manifests)
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class ResizeCheckpointContext:
    """Elasticity metadata embedded in a checkpoint manifest during resize.

    The controller and subsequent restores use this to understand whether the
    checkpoint was produced as part of a resize event.
    """

    active_worker_count: int
    target_worker_count: int
    resize_direction: ResizeDirection
    resize_reason: str
    in_place_shrink_supported: bool

    def to_manifest_fields(self) -> dict[str, Any]:
        """Return the dict to merge into a :class:`CheckpointManifest`."""
        return {
            "resizeActiveWorkerCount": self.active_worker_count,
            "resizeTargetWorkerCount": self.target_worker_count,
            "resizeDirection": self.resize_direction.value,
            "resizeReason": self.resize_reason,
            "resizeInPlaceShrinkSupported": self.in_place_shrink_supported,
        }


def build_resize_checkpoint_context(
    config: ElasticConfig,
    reason: str = "",
) -> ResizeCheckpointContext | None:
    """Create a :class:`ResizeCheckpointContext` if a resize is active.

    Returns *None* when no resize is requested.
    """
    if not config.resize_requested:
        return None
    return ResizeCheckpointContext(
        active_worker_count=config.current_worker_count,
        target_worker_count=config.target_worker_count,
        resize_direction=config.resize_direction,
        resize_reason=reason or (
            f"{config.resize_direction.value.lower()}"
            f" from {config.current_worker_count}"
            f" to {config.target_worker_count}"
        ),
        in_place_shrink_supported=config.supports_in_place_shrink,
    )


# ---------------------------------------------------------------------------
# Resize signal file (runtime -> controller communication)
# ---------------------------------------------------------------------------

RESIZE_SIGNAL_FILENAME = "resize-signal.json"


def write_resize_signal(
    signal_dir: Path,
    outcome: ResizeOutcome,
    *,
    checkpoint_id: str | None = None,
    manifest_uri: str | None = None,
) -> Path:
    """Write a resize signal file for the controller.

    The controller reads this to determine:

    - The outcome of the resize request.
    - Whether to proceed with in-place shrink or fall back to C&R.
    - The checkpoint reference, if one was produced.
    """
    signal_path = signal_dir / RESIZE_SIGNAL_FILENAME
    signal_dir.mkdir(parents=True, exist_ok=True)
    payload = outcome.to_dict()
    if checkpoint_id:
        payload["checkpointID"] = checkpoint_id
    if manifest_uri:
        payload["manifestURI"] = manifest_uri
    signal_path.write_text(
        json.dumps(payload, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return signal_path


def read_resize_signal(signal_dir: Path) -> dict[str, Any] | None:
    """Read a resize signal file if present.  Returns *None* if absent."""
    signal_path = signal_dir / RESIZE_SIGNAL_FILENAME
    if not signal_path.exists():
        return None
    return json.loads(signal_path.read_text(encoding="utf-8"))
