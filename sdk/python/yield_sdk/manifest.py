from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

FORMAT_VERSION = "yield-sdk.manifest/v1alpha1"
CHECKPOINT_FORMAT_DCP_V1 = "dcp/v1"


class ManifestValidationError(ValueError):
    """Raised when a manifest is incomplete or malformed."""


def _require_non_empty(value: str | None, field_name: str) -> None:
    if value is None or not str(value).strip():
        raise ManifestValidationError(f"{field_name} must be a non-empty string")


@dataclass(frozen=True)
class ArtifactEntry:
    name: str
    relative_path: str
    object_uri: str
    size_bytes: int
    digest_value: str
    digest_algorithm: str = "sha256"

    def validate(self) -> None:
        _require_non_empty(self.name, "artifacts[].name")
        _require_non_empty(self.relative_path, "artifacts[].relativePath")
        _require_non_empty(self.object_uri, "artifacts[].objectURI")
        _require_non_empty(self.digest_algorithm, "artifacts[].digestAlgorithm")
        _require_non_empty(self.digest_value, "artifacts[].digestValue")
        if self.size_bytes < 0:
            raise ManifestValidationError("artifacts[].sizeBytes must be >= 0")

    def to_dict(self) -> dict[str, Any]:
        self.validate()
        return {
            "name": self.name,
            "relativePath": self.relative_path,
            "objectURI": self.object_uri,
            "sizeBytes": self.size_bytes,
            "digestAlgorithm": self.digest_algorithm,
            "digestValue": self.digest_value,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "ArtifactEntry":
        entry = cls(
            name=payload["name"],
            relative_path=payload.get("relativePath", payload.get("relative_path", "")),
            object_uri=payload.get("objectURI", payload.get("object_uri", "")),
            size_bytes=int(payload.get("sizeBytes", payload.get("size_bytes", -1))),
            digest_algorithm=payload.get("digestAlgorithm", payload.get("digest_algorithm", "sha256")),
            digest_value=payload.get("digestValue", payload.get("digest_value", "")),
        )
        entry.validate()
        return entry


@dataclass
class CheckpointManifest:
    checkpoint_id: str
    cluster_identity: str
    rtj_identity: str
    run_attempt: int
    global_step: int
    wall_clock_timestamp: str
    image_identity: str
    code_version_identity: str
    runtime_mode: str
    world_size: int
    gpu_shape: str
    optimizer_mode: str
    sharding_mode: str
    producer_version: str
    storage_root_uri: str
    artifacts: list[ArtifactEntry] = field(default_factory=list)
    format_version: str = FORMAT_VERSION
    completion_timestamp: str | None = None
    manifest_uri: str | None = None
    dcp_metadata: dict[str, Any] | None = None
    replica_count: int | None = None
    rank_layout: list[dict[str, Any]] | None = None
    leader_count: int | None = None
    worker_count: int | None = None
    checkpoint_format_version: str | None = None
    cross_size_restore_supported: bool | None = None

    # Phase 8: device profile fingerprint from DRA device spec.
    # SHA256 hash of the canonical sorted device class + selector entries.
    # Empty/None when DRA is not configured (Phase 7 backward compatibility).
    device_profile_fingerprint: str | None = None

    def validate(self) -> None:
        _require_non_empty(self.checkpoint_id, "checkpointID")
        _require_non_empty(self.cluster_identity, "clusterIdentity")
        _require_non_empty(self.rtj_identity, "rtjIdentity")
        _require_non_empty(self.wall_clock_timestamp, "wallClockTimestamp")
        _require_non_empty(self.image_identity, "imageIdentity")
        _require_non_empty(self.code_version_identity, "codeVersionIdentity")
        _require_non_empty(self.runtime_mode, "runtimeMode")
        _require_non_empty(self.gpu_shape, "gpuShape")
        _require_non_empty(self.optimizer_mode, "optimizerMode")
        _require_non_empty(self.sharding_mode, "shardingMode")
        _require_non_empty(self.format_version, "formatVersion")
        _require_non_empty(self.producer_version, "producerVersion")
        _require_non_empty(self.storage_root_uri, "storageRootURI")
        _require_non_empty(self.completion_timestamp, "completionTimestamp")

        if self.run_attempt < 1:
            raise ManifestValidationError("runAttempt must be >= 1")
        if self.global_step < 0:
            raise ManifestValidationError("globalStep must be >= 0")
        if self.world_size < 1:
            raise ManifestValidationError("worldSize must be >= 1")
        if not self.artifacts:
            raise ManifestValidationError("artifacts must contain at least one required object")
        for artifact in self.artifacts:
            artifact.validate()

    def to_dict(self) -> dict[str, Any]:
        self.validate()
        payload: dict[str, Any] = {
            "checkpointID": self.checkpoint_id,
            "clusterIdentity": self.cluster_identity,
            "rtjIdentity": self.rtj_identity,
            "runAttempt": self.run_attempt,
            "globalStep": self.global_step,
            "wallClockTimestamp": self.wall_clock_timestamp,
            "imageIdentity": self.image_identity,
            "codeVersionIdentity": self.code_version_identity,
            "runtimeMode": self.runtime_mode,
            "worldSize": self.world_size,
            "gpuShape": self.gpu_shape,
            "optimizerMode": self.optimizer_mode,
            "shardingMode": self.sharding_mode,
            "formatVersion": self.format_version,
            "producerVersion": self.producer_version,
            "storageRootURI": self.storage_root_uri,
            "artifacts": [artifact.to_dict() for artifact in self.artifacts],
            "completionTimestamp": self.completion_timestamp,
        }
        if self.manifest_uri:
            payload["manifestURI"] = self.manifest_uri
        if self.dcp_metadata:
            payload["dcpMetadata"] = self.dcp_metadata
        if self.replica_count is not None:
            payload["replicaCount"] = self.replica_count
        if self.rank_layout is not None:
            payload["rankLayout"] = self.rank_layout
        if self.leader_count is not None:
            payload["leaderCount"] = self.leader_count
        if self.worker_count is not None:
            payload["workerCount"] = self.worker_count
        if self.checkpoint_format_version is not None:
            payload["checkpointFormatVersion"] = self.checkpoint_format_version
        if self.cross_size_restore_supported is not None:
            payload["crossSizeRestoreSupported"] = self.cross_size_restore_supported
        if self.device_profile_fingerprint is not None:
            payload["deviceProfileFingerprint"] = self.device_profile_fingerprint
        return payload

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), indent=2, sort_keys=True) + "\n"

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "CheckpointManifest":
        manifest = cls(
            checkpoint_id=payload["checkpointID"],
            cluster_identity=payload["clusterIdentity"],
            rtj_identity=payload["rtjIdentity"],
            run_attempt=int(payload["runAttempt"]),
            global_step=int(payload["globalStep"]),
            wall_clock_timestamp=payload["wallClockTimestamp"],
            image_identity=payload["imageIdentity"],
            code_version_identity=payload["codeVersionIdentity"],
            runtime_mode=payload["runtimeMode"],
            world_size=int(payload["worldSize"]),
            gpu_shape=payload["gpuShape"],
            optimizer_mode=payload["optimizerMode"],
            sharding_mode=payload["shardingMode"],
            format_version=payload.get("formatVersion", FORMAT_VERSION),
            producer_version=payload["producerVersion"],
            storage_root_uri=payload["storageRootURI"],
            artifacts=[ArtifactEntry.from_dict(item) for item in payload.get("artifacts", [])],
            completion_timestamp=payload.get("completionTimestamp"),
            manifest_uri=payload.get("manifestURI"),
            dcp_metadata=payload.get("dcpMetadata"),
            replica_count=payload.get("replicaCount"),
            rank_layout=payload.get("rankLayout"),
            leader_count=payload.get("leaderCount"),
            worker_count=payload.get("workerCount"),
            checkpoint_format_version=payload.get("checkpointFormatVersion"),
            cross_size_restore_supported=payload.get("crossSizeRestoreSupported"),
            device_profile_fingerprint=payload.get("deviceProfileFingerprint"),
        )
        manifest.validate()
        return manifest

    @classmethod
    def from_json(cls, raw_text: str) -> "CheckpointManifest":
        try:
            payload = json.loads(raw_text)
        except json.JSONDecodeError as exc:
            raise ManifestValidationError(f"manifest is not valid JSON: {exc}") from exc
        if not isinstance(payload, dict):
            raise ManifestValidationError("manifest root must be a JSON object")
        return cls.from_dict(payload)
