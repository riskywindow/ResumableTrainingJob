from __future__ import annotations

import hashlib
import json
import shutil
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Mapping

import logging

from .manifest import CHECKPOINT_FORMAT_DCP_V1, FORMAT_VERSION, ArtifactEntry, CheckpointManifest
from .runtime import PACKAGE_VERSION, RuntimeConfig
from .storage import S3Storage

logger = logging.getLogger("yield_sdk.checkpoint")

RESTORE_MODE_SAME_SIZE = "SameSize"
RESTORE_MODE_RESHARD = "Reshard"


def _utc_now() -> datetime:
    return datetime.now(timezone.utc)


def _isoformat(value: datetime | None = None) -> str:
    current = value or _utc_now()
    return current.replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _import_torch():
    try:
        import torch
    except ModuleNotFoundError as exc:
        raise RuntimeError("torch is required for DCP checkpoint operations") from exc
    return torch


def _import_dcp():
    torch = _import_torch()
    try:
        import torch.distributed.checkpoint as dcp
    except ModuleNotFoundError as exc:
        raise RuntimeError("torch.distributed.checkpoint is required for Phase 1 checkpoints") from exc
    return torch, dcp


def _rank() -> int:
    try:
        import torch.distributed as dist
    except ModuleNotFoundError:
        return 0
    if dist.is_available() and dist.is_initialized():
        return int(dist.get_rank())
    return 0


def _world_size() -> int:
    try:
        import torch.distributed as dist
    except ModuleNotFoundError:
        return 1
    if dist.is_available() and dist.is_initialized():
        return int(dist.get_world_size())
    return 1


def _barrier() -> None:
    try:
        import torch.distributed as dist
    except ModuleNotFoundError:
        return
    if dist.is_available() and dist.is_initialized():
        dist.barrier()


def _is_rank_zero() -> bool:
    return _rank() == 0


def _json_to_tensor(payload: Mapping[str, Any]):
    torch = _import_torch()
    raw_bytes = json.dumps(dict(payload), sort_keys=True).encode("utf-8")
    return torch.tensor(list(raw_bytes), dtype=torch.uint8)


def _tensor_to_json(tensor) -> dict[str, Any]:
    raw = bytes(tensor.cpu().tolist())
    if not raw:
        return {}
    return json.loads(raw.decode("utf-8"))


def _capture_rng_state() -> dict[str, Any]:
    torch = _import_torch()
    state: dict[str, Any] = {"cpu_rng_state": torch.get_rng_state()}
    if torch.cuda.is_available():
        cuda_states = torch.cuda.get_rng_state_all()
        for index, tensor in enumerate(cuda_states):
            state[f"cuda_rng_state_{index}"] = tensor
    return state


def _restore_rng_state(state: Mapping[str, Any]) -> None:
    torch = _import_torch()
    cpu_state = state.get("cpu_rng_state")
    if cpu_state is not None:
        torch.set_rng_state(cpu_state.cpu())

    cuda_entries: list[tuple[int, Any]] = []
    for key, value in state.items():
        if not key.startswith("cuda_rng_state_"):
            continue
        index = int(key.rsplit("_", 1)[-1])
        cuda_entries.append((index, value))

    if cuda_entries and torch.cuda.is_available():
        ordered = [tensor.cpu() for _, tensor in sorted(cuda_entries, key=lambda item: item[0])]
        torch.cuda.set_rng_state_all(ordered)


class _StateDictAdapter:
    def __init__(self, wrapped: Any):
        self.wrapped = wrapped

    def state_dict(self) -> dict[str, Any]:
        return self.wrapped.state_dict()

    def load_state_dict(self, state_dict: Mapping[str, Any]) -> None:
        self.wrapped.load_state_dict(state_dict)


class _JSONStateAdapter:
    def __init__(self, payload: Mapping[str, Any] | None = None):
        self.payload = dict(payload or {})

    def state_dict(self) -> dict[str, Any]:
        return {"payload": _json_to_tensor(self.payload)}

    def load_state_dict(self, state_dict: Mapping[str, Any]) -> None:
        self.payload = _tensor_to_json(state_dict["payload"])


class _TensorStateAdapter:
    def __init__(self, payload: Mapping[str, Any] | None = None):
        self.payload = dict(payload or {})

    def state_dict(self) -> dict[str, Any]:
        return dict(self.payload)

    def load_state_dict(self, state_dict: Mapping[str, Any]) -> None:
        self.payload = dict(state_dict)


@dataclass
class CheckpointSaveResult:
    manifest: CheckpointManifest
    staging_dir: Path
    manifest_path: Path


@dataclass
class CheckpointRestoreResult:
    manifest: CheckpointManifest
    restore_dir: Path
    step: int
    trainer_state: dict[str, Any]
    restore_mode: str = RESTORE_MODE_SAME_SIZE


def create_checkpoint_id(run_attempt: int, global_step: int, *, now: datetime | None = None) -> str:
    timestamp = (now or _utc_now()).strftime("%Y%m%dT%H%M%SZ")
    return f"ckpt-{timestamp}-a{run_attempt}-s{global_step}"


def _prepare_shared_dir(root: Path, directory_name: str) -> Path:
    shared_dir = root / directory_name
    if _is_rank_zero():
        if shared_dir.exists():
            shutil.rmtree(shared_dir)
        shared_dir.mkdir(parents=True, exist_ok=True)
    _barrier()
    return shared_dir


def _write_runtime_metadata(path: Path, *, runtime: RuntimeConfig, checkpoint_id: str, step: int, created_at: str) -> None:
    payload = {
        "checkpointID": checkpoint_id,
        "clusterIdentity": runtime.cluster_identity,
        "rtjIdentity": runtime.rtj_identity,
        "runAttempt": runtime.run_attempt,
        "globalStep": step,
        "createdAt": created_at,
        "runtimeMode": runtime.runtime_mode,
        "worldSize": runtime.world_size,
        "gpuShape": runtime.gpu_shape,
        "imageIdentity": runtime.image_identity,
        "codeVersionIdentity": runtime.code_version_identity,
        "optimizerMode": runtime.optimizer_mode,
        "shardingMode": runtime.sharding_mode,
        "producerVersion": PACKAGE_VERSION,
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _write_trainer_metadata(path: Path, payload: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(dict(payload), indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _build_manifest(
    *,
    runtime: RuntimeConfig,
    checkpoint_id: str,
    global_step: int,
    created_at: str,
    completion_timestamp: str,
    storage_root_uri: str,
    manifest_uri: str,
    artifacts: list[ArtifactEntry],
) -> CheckpointManifest:
    ws = _world_size()
    rank_layout = [{"rank": rank, "localRank": rank} for rank in range(ws)]
    return CheckpointManifest(
        checkpoint_id=checkpoint_id,
        cluster_identity=runtime.cluster_identity,
        rtj_identity=runtime.rtj_identity,
        run_attempt=runtime.run_attempt,
        global_step=global_step,
        wall_clock_timestamp=created_at,
        image_identity=runtime.image_identity,
        code_version_identity=runtime.code_version_identity,
        runtime_mode=runtime.runtime_mode,
        world_size=runtime.world_size,
        gpu_shape=runtime.gpu_shape,
        optimizer_mode=runtime.optimizer_mode,
        sharding_mode=runtime.sharding_mode,
        producer_version=PACKAGE_VERSION,
        storage_root_uri=storage_root_uri,
        artifacts=artifacts,
        completion_timestamp=completion_timestamp,
        manifest_uri=manifest_uri,
        dcp_metadata={"backend": "torch.distributed.checkpoint"},
        replica_count=ws,
        rank_layout=rank_layout,
        leader_count=0,
        worker_count=runtime.world_size,
        checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
        cross_size_restore_supported=True,
    )


def _validate_restore_manifest(manifest: CheckpointManifest, runtime: RuntimeConfig) -> str:
    """Validate manifest compatibility. Returns the restore mode (SameSize or Reshard).

    Raises RuntimeError if the manifest is incompatible with the runtime.
    """
    mismatches: list[str] = []
    if manifest.cluster_identity != runtime.cluster_identity:
        mismatches.append("clusterIdentity")
    if manifest.rtj_identity != runtime.rtj_identity:
        mismatches.append("rtjIdentity")
    if manifest.runtime_mode != runtime.runtime_mode:
        mismatches.append("runtimeMode")
    if manifest.gpu_shape != runtime.gpu_shape:
        mismatches.append("gpuShape")
    if manifest.image_identity != runtime.image_identity:
        mismatches.append("imageIdentity")
    if manifest.code_version_identity != runtime.code_version_identity:
        mismatches.append("codeVersionIdentity")
    if manifest.optimizer_mode != runtime.optimizer_mode:
        mismatches.append("optimizerMode")
    if manifest.sharding_mode != runtime.sharding_mode:
        mismatches.append("shardingMode")
    if manifest.format_version != FORMAT_VERSION:
        mismatches.append("formatVersion")

    world_size_differs = manifest.world_size != runtime.world_size
    if world_size_differs:
        if not runtime.allow_world_size_change:
            mismatches.append("worldSize")
        elif not manifest.cross_size_restore_supported:
            mismatches.append("worldSize (cross-size restore not supported by checkpoint)")

    if mismatches:
        raise RuntimeError(f"restore manifest is incompatible with the runtime configuration: {', '.join(mismatches)}")

    return RESTORE_MODE_RESHARD if world_size_differs else RESTORE_MODE_SAME_SIZE


def _upload_staged_files(
    *,
    storage: S3Storage,
    staging_dir: Path,
    storage_root_uri: str,
) -> list[ArtifactEntry]:
    artifacts: list[ArtifactEntry] = []
    for local_path in sorted(path for path in staging_dir.rglob("*") if path.is_file()):
        relative_path = local_path.relative_to(staging_dir).as_posix()
        object_uri = f"{storage_root_uri.rstrip('/')}/{relative_path}"
        content_type = "application/json" if local_path.suffix == ".json" else None
        storage.upload_file(local_path, object_uri, content_type=content_type)
        artifacts.append(
            ArtifactEntry(
                name=relative_path.replace("/", "-"),
                relative_path=relative_path,
                object_uri=object_uri,
                size_bytes=local_path.stat().st_size,
                digest_value=_sha256(local_path),
            )
        )
    return artifacts


def save_checkpoint(
    *,
    model: Any,
    optimizer: Any,
    runtime: RuntimeConfig,
    storage: S3Storage,
    step: int,
    trainer_state: Mapping[str, Any] | None = None,
    checkpoint_id: str | None = None,
) -> CheckpointSaveResult:
    torch, dcp = _import_dcp()
    base_model = getattr(model, "module", model)
    created_at = _isoformat()
    checkpoint_name = checkpoint_id or create_checkpoint_id(runtime.run_attempt, step)
    staging_dir = _prepare_shared_dir(runtime.staging_root, checkpoint_name)
    data_dir = staging_dir / "data"
    metadata_path = staging_dir / "metadata" / "runtime.json"
    trainer_state_path = staging_dir / "metadata" / "trainer-state.json"
    manifest_path = staging_dir / "manifest.json"
    data_dir.mkdir(parents=True, exist_ok=True)

    trainer_payload = dict(trainer_state or {})
    trainer_payload["step"] = step
    app_state = {
        "model": _StateDictAdapter(base_model),
        "optimizer": _StateDictAdapter(optimizer),
        "rng": _TensorStateAdapter(_capture_rng_state()),
    }

    dcp.save(state_dict=app_state, storage_writer=dcp.FileSystemWriter(str(data_dir)))
    _barrier()

    if _is_rank_zero():
        _write_runtime_metadata(
            metadata_path,
            runtime=runtime,
            checkpoint_id=checkpoint_name,
            step=step,
            created_at=created_at,
        )
        _write_trainer_metadata(trainer_state_path, trainer_payload)
        storage_root_uri = runtime.checkpoint_root_uri(checkpoint_name)
        manifest_uri = runtime.manifest_uri_for(checkpoint_name)
        artifacts = _upload_staged_files(storage=storage, staging_dir=staging_dir, storage_root_uri=storage_root_uri)
        manifest = _build_manifest(
            runtime=runtime,
            checkpoint_id=checkpoint_name,
            global_step=step,
            created_at=created_at,
            completion_timestamp=_isoformat(),
            storage_root_uri=storage_root_uri,
            manifest_uri=manifest_uri,
            artifacts=artifacts,
        )
        manifest_path.write_text(manifest.to_json(), encoding="utf-8")
        storage.upload_bytes(manifest.to_json().encode("utf-8"), manifest_uri, content_type="application/json")

    _barrier()
    manifest = CheckpointManifest.from_json(manifest_path.read_text(encoding="utf-8"))
    return CheckpointSaveResult(manifest=manifest, staging_dir=staging_dir, manifest_path=manifest_path)


def restore_checkpoint(
    *,
    model: Any,
    optimizer: Any,
    runtime: RuntimeConfig,
    storage: S3Storage,
    manifest_uri: str | None = None,
) -> CheckpointRestoreResult:
    _, dcp = _import_dcp()
    base_model = getattr(model, "module", model)
    selected_manifest_uri = manifest_uri or runtime.restore_manifest_uri
    if not selected_manifest_uri:
        raise RuntimeError("restore_checkpoint requires a manifest URI")

    restore_dir = _prepare_shared_dir(runtime.restore_root, "restore")
    local_manifest_path = restore_dir / "manifest.json"

    restore_mode = RESTORE_MODE_SAME_SIZE

    if _is_rank_zero():
        manifest = CheckpointManifest.from_json(storage.download_bytes(selected_manifest_uri).decode("utf-8"))
        restore_mode = _validate_restore_manifest(manifest, runtime)
        local_manifest_path.write_text(manifest.to_json(), encoding="utf-8")
        for artifact in manifest.artifacts:
            target_path = restore_dir / artifact.relative_path
            storage.download_file(artifact.object_uri, target_path)

    _barrier()
    manifest = CheckpointManifest.from_json(local_manifest_path.read_text(encoding="utf-8"))
    restore_mode = _validate_restore_manifest(manifest, runtime)

    logger.info(
        "restoring checkpoint %s: manifest_world_size=%d runtime_world_size=%d restore_mode=%s",
        manifest.checkpoint_id, manifest.world_size, runtime.world_size, restore_mode,
    )

    rng_adapter = _TensorStateAdapter()
    app_state = {
        "model": _StateDictAdapter(base_model),
        "optimizer": _StateDictAdapter(optimizer),
        "rng": rng_adapter,
    }
    dcp.load(state_dict=app_state, storage_reader=dcp.FileSystemReader(str(restore_dir / "data")))
    if restore_mode == RESTORE_MODE_SAME_SIZE:
        _restore_rng_state(rng_adapter.payload)

    trainer_state_path = restore_dir / "metadata" / "trainer-state.json"
    trainer_state = json.loads(trainer_state_path.read_text(encoding="utf-8"))
    step = int(trainer_state.get("step", 0))
    return CheckpointRestoreResult(
        manifest=manifest,
        restore_dir=restore_dir,
        step=step,
        trainer_state=trainer_state,
        restore_mode=restore_mode,
    )
