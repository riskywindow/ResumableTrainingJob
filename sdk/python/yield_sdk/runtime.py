from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

PACKAGE_VERSION = "0.1.0"


def choose_backend(preferred: str | None = None) -> str:
    if preferred:
        return preferred

    try:
        import torch
    except ModuleNotFoundError:
        return "gloo"

    if torch.cuda.is_available():
        return "nccl"
    return "gloo"


@dataclass(frozen=True)
class RuntimeConfig:
    cluster_identity: str
    rtj_identity: str
    run_attempt: int
    runtime_mode: str
    world_size: int
    gpu_shape: str
    image_identity: str
    code_version_identity: str
    optimizer_mode: str
    sharding_mode: str
    checkpoint_storage_uri: str
    staging_root: Path
    restore_root: Path
    control_file: Path | None = None
    restore_manifest_uri: str | None = None
    yield_marker_path: Path | None = None
    yield_marker_uri: str | None = None
    original_world_size: int | None = None
    allow_world_size_change: bool = False

    def checkpoint_root_uri(self, checkpoint_id: str) -> str:
        root = self.checkpoint_storage_uri.rstrip("/")
        return f"{root}/checkpoints/{checkpoint_id}"

    def manifest_uri_for(self, checkpoint_id: str) -> str:
        root = self.checkpoint_storage_uri.rstrip("/")
        return f"{root}/manifests/{checkpoint_id}.manifest.json"

    @classmethod
    def from_env(cls) -> "RuntimeConfig":
        cluster_identity = os.environ.get("YIELD_SDK_CLUSTER_IDENTITY", "kind-phase1")
        rtj_identity = os.environ.get("YIELD_SDK_RTJ_IDENTITY", "phase1-rtj")
        run_attempt = int(os.environ.get("YIELD_SDK_RUN_ATTEMPT", "1"))
        runtime_mode = os.environ.get("YIELD_SDK_RUNTIME_MODE", "DDP")
        world_size = int(os.environ.get("YIELD_SDK_WORLD_SIZE", os.environ.get("WORLD_SIZE", "1")))
        gpu_shape = os.environ.get("YIELD_SDK_GPU_SHAPE", "cpu")
        image_identity = os.environ.get("YIELD_SDK_IMAGE_IDENTITY", "local/fixture:dev")
        code_version_identity = os.environ.get("YIELD_SDK_CODE_VERSION", "workspace")
        optimizer_mode = os.environ.get("YIELD_SDK_OPTIMIZER_MODE", "adamw")
        sharding_mode = os.environ.get("YIELD_SDK_SHARDING_MODE", "replicated-optimizer-state")
        checkpoint_storage_uri = os.environ.get("YIELD_SDK_STORAGE_URI", "s3://phase1-checkpoints/rtj")
        staging_root = Path(os.environ.get("YIELD_SDK_STAGING_ROOT", "/tmp/yield-sdk/staging"))
        restore_root = Path(os.environ.get("YIELD_SDK_RESTORE_ROOT", "/tmp/yield-sdk/restore"))
        control_file = os.environ.get("YIELD_SDK_CONTROL_FILE")
        restore_manifest_uri = os.environ.get("YIELD_SDK_RESTORE_MANIFEST_URI")
        yield_marker_path = os.environ.get("YIELD_SDK_YIELD_MARKER_PATH")
        yield_marker_uri = os.environ.get("YIELD_SDK_YIELD_MARKER_URI")
        original_ws = os.environ.get("YIELD_SDK_ORIGINAL_WORLD_SIZE")
        original_world_size = int(original_ws) if original_ws else None
        allow_world_size_change = os.environ.get("YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE", "").lower() in ("1", "true", "yes")

        return cls(
            cluster_identity=cluster_identity,
            rtj_identity=rtj_identity,
            run_attempt=run_attempt,
            runtime_mode=runtime_mode,
            world_size=world_size,
            gpu_shape=gpu_shape,
            image_identity=image_identity,
            code_version_identity=code_version_identity,
            optimizer_mode=optimizer_mode,
            sharding_mode=sharding_mode,
            checkpoint_storage_uri=checkpoint_storage_uri,
            staging_root=staging_root,
            restore_root=restore_root,
            control_file=Path(control_file) if control_file else None,
            restore_manifest_uri=restore_manifest_uri,
            yield_marker_path=Path(yield_marker_path) if yield_marker_path else None,
            yield_marker_uri=yield_marker_uri,
            original_world_size=original_world_size,
            allow_world_size_change=allow_world_size_change,
        )
