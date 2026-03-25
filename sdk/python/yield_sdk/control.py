from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

RUNNING = "Running"
PAUSED = "Paused"
VALID_STATES = {RUNNING, PAUSED}


class ControlFileError(ValueError):
    """Raised when the mounted control file cannot be parsed safely."""


@dataclass(frozen=True)
class ControlRecord:
    desired_state: str = RUNNING
    request_id: str | None = None
    updated_at: str | None = None
    metadata: dict[str, Any] | None = None

    @property
    def yield_requested(self) -> bool:
        return self.desired_state == PAUSED


def _normalise_payload(raw_payload: Any) -> dict[str, Any]:
    if raw_payload is None:
        return {}
    if isinstance(raw_payload, str):
        stripped = raw_payload.strip()
        if not stripped:
            return {}
        if stripped in VALID_STATES:
            return {"desiredState": stripped}
        try:
            parsed = json.loads(stripped)
        except json.JSONDecodeError as exc:
            raise ControlFileError(f"control file is not valid JSON: {exc}") from exc
        return _normalise_payload(parsed)
    if not isinstance(raw_payload, dict):
        raise ControlFileError("control file must contain a JSON object or a plain desired-state string")
    return raw_payload


def load_control_record(path: str | Path | None) -> ControlRecord:
    if path is None:
        return ControlRecord()

    control_path = Path(path)
    if not control_path.exists():
        return ControlRecord()

    raw_text = control_path.read_text(encoding="utf-8").strip()
    if not raw_text:
        return ControlRecord()

    payload = _normalise_payload(raw_text)
    desired_state = payload.get("desiredState", payload.get("desired_state", RUNNING))
    if desired_state not in VALID_STATES:
        raise ControlFileError(
            f"unsupported desired state {desired_state!r}; expected one of {sorted(VALID_STATES)}"
        )

    request_id = payload.get("requestId", payload.get("request_id"))
    updated_at = payload.get("updatedAt", payload.get("updated_at"))
    metadata = {
        key: value
        for key, value in payload.items()
        if key not in {"desiredState", "desired_state", "requestId", "request_id", "updatedAt", "updated_at"}
    }

    return ControlRecord(
        desired_state=desired_state,
        request_id=request_id,
        updated_at=updated_at,
        metadata=metadata or None,
    )


class ControlFile:
    """Thin helper for polling the mounted Phase 1 control file."""

    def __init__(self, path: str | Path | None):
        self.path = Path(path) if path is not None else None

    def read(self) -> ControlRecord:
        return load_control_record(self.path)

    def yield_requested(self) -> bool:
        return self.read().yield_requested
