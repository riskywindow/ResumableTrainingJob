from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import torch

from yield_sdk.checkpoint import (
    RESTORE_MODE_RESHARD,
    RESTORE_MODE_SAME_SIZE,
    _validate_restore_manifest,
    create_checkpoint_id,
    save_checkpoint,
)
from yield_sdk.manifest import CHECKPOINT_FORMAT_DCP_V1, CheckpointManifest, ArtifactEntry
from yield_sdk.runtime import RuntimeConfig
from yield_sdk.storage import S3Storage, S3StorageConfig


class _FakeResponse:
    def __init__(self, payload: bytes):
        self._payload = payload

    def read(self) -> bytes:
        return self._payload

    def close(self) -> None:
        return None

    def release_conn(self) -> None:
        return None


class _FakeStat:
    def __init__(self, size: int):
        self.size = size


class _FakeClient:
    def __init__(self):
        self.objects: dict[tuple[str, str], bytes] = {}
        self.buckets: set[str] = set()

    def bucket_exists(self, bucket_name: str) -> bool:
        return bucket_name in self.buckets

    def make_bucket(self, bucket_name: str):
        self.buckets.add(bucket_name)

    def fput_object(self, bucket_name: str, object_name: str, file_path: str, content_type: str | None = None):
        self.buckets.add(bucket_name)
        self.objects[(bucket_name, object_name)] = Path(file_path).read_bytes()

    def fget_object(self, bucket_name: str, object_name: str, file_path: str):
        Path(file_path).parent.mkdir(parents=True, exist_ok=True)
        Path(file_path).write_bytes(self.objects[(bucket_name, object_name)])

    def put_object(self, bucket_name: str, object_name: str, data, length: int, content_type: str | None = None):
        self.buckets.add(bucket_name)
        self.objects[(bucket_name, object_name)] = data.read(length)

    def get_object(self, bucket_name: str, object_name: str):
        return _FakeResponse(self.objects[(bucket_name, object_name)])

    def stat_object(self, bucket_name: str, object_name: str):
        return _FakeStat(len(self.objects[(bucket_name, object_name)]))


def _make_storage() -> S3Storage:
    return S3Storage(
        S3StorageConfig(endpoint="minio.example:9000", access_key="minio", secret_key="miniopass"),
        client=_FakeClient(),
        auto_create_bucket=True,
    )


def _make_runtime(base: Path, **overrides) -> RuntimeConfig:
    defaults = dict(
        cluster_identity="phase1-kind",
        rtj_identity="demo-rtj",
        run_attempt=1,
        runtime_mode="DDP",
        world_size=1,
        gpu_shape="cpu",
        image_identity="local/fixture:dev",
        code_version_identity="git:abc123",
        optimizer_mode="adamw",
        sharding_mode="replicated-optimizer-state",
        checkpoint_storage_uri="s3://bucket/demo-rtj",
        staging_root=base / "staging",
        restore_root=base / "restore",
    )
    defaults.update(overrides)
    return RuntimeConfig(**defaults)


def _make_manifest(**overrides) -> CheckpointManifest:
    defaults = dict(
        checkpoint_id="ckpt-test",
        cluster_identity="phase1-kind",
        rtj_identity="demo-rtj",
        run_attempt=1,
        global_step=5,
        wall_clock_timestamp="2026-03-21T01:02:03Z",
        image_identity="local/fixture:dev",
        code_version_identity="git:abc123",
        runtime_mode="DDP",
        world_size=1,
        gpu_shape="cpu",
        optimizer_mode="adamw",
        sharding_mode="replicated-optimizer-state",
        producer_version="0.1.0",
        storage_root_uri="s3://bucket/demo-rtj/checkpoints/ckpt-test",
        completion_timestamp="2026-03-21T01:02:05Z",
        artifacts=[ArtifactEntry(
            name="data", relative_path="data/0", object_uri="s3://bucket/data/0",
            size_bytes=8, digest_value="a" * 64,
        )],
        cross_size_restore_supported=True,
        checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
        leader_count=0,
        worker_count=1,
    )
    defaults.update(overrides)
    return CheckpointManifest(**defaults)


class CheckpointIdTests(unittest.TestCase):
    def test_checkpoint_id_format(self) -> None:
        from datetime import datetime, timezone
        ts = datetime(2026, 3, 21, 12, 0, 30, tzinfo=timezone.utc)
        cid = create_checkpoint_id(1, 5, now=ts)
        self.assertEqual(cid, "ckpt-20260321T120030Z-a1-s5")

    def test_checkpoint_id_includes_attempt_and_step(self) -> None:
        cid = create_checkpoint_id(3, 100)
        self.assertIn("-a3-", cid)
        self.assertIn("-s100", cid)


class SaveCheckpointTests(unittest.TestCase):
    def test_saved_manifest_has_phase3_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            runtime = _make_runtime(base)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=runtime,
                storage=storage, step=1,
            )

            m = result.manifest
            self.assertEqual(m.leader_count, 0)
            self.assertEqual(m.worker_count, 1)
            self.assertEqual(m.checkpoint_format_version, CHECKPOINT_FORMAT_DCP_V1)
            self.assertTrue(m.cross_size_restore_supported)
            self.assertIsNotNone(m.completion_timestamp)
            self.assertGreater(len(m.artifacts), 0)

    def test_saved_manifest_preserves_phase2_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            runtime = _make_runtime(base)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=runtime,
                storage=storage, step=7, trainer_state={"loss": 0.1},
            )

            m = result.manifest
            self.assertEqual(m.cluster_identity, "phase1-kind")
            self.assertEqual(m.rtj_identity, "demo-rtj")
            self.assertEqual(m.runtime_mode, "DDP")
            self.assertEqual(m.world_size, 1)
            self.assertEqual(m.gpu_shape, "cpu")
            self.assertEqual(m.global_step, 7)


class ValidateRestoreManifestTests(unittest.TestCase):
    def test_same_size_returns_same_size_mode(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(world_size=4)
            runtime = _make_runtime(Path(tmpdir), world_size=4)
            mode = _validate_restore_manifest(manifest, runtime)
            self.assertEqual(mode, RESTORE_MODE_SAME_SIZE)

    def test_different_size_with_allow_returns_reshard(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(world_size=4, cross_size_restore_supported=True)
            runtime = _make_runtime(
                Path(tmpdir), world_size=8, allow_world_size_change=True,
            )
            mode = _validate_restore_manifest(manifest, runtime)
            self.assertEqual(mode, RESTORE_MODE_RESHARD)

    def test_different_size_without_allow_raises(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(world_size=4)
            runtime = _make_runtime(Path(tmpdir), world_size=8)
            with self.assertRaises(RuntimeError) as ctx:
                _validate_restore_manifest(manifest, runtime)
            self.assertIn("worldSize", str(ctx.exception))

    def test_different_size_without_cross_support_raises(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(world_size=4, cross_size_restore_supported=False)
            runtime = _make_runtime(
                Path(tmpdir), world_size=8, allow_world_size_change=True,
            )
            with self.assertRaises(RuntimeError) as ctx:
                _validate_restore_manifest(manifest, runtime)
            self.assertIn("cross-size restore not supported", str(ctx.exception))

    def test_different_size_with_none_cross_support_raises(self) -> None:
        """Phase 2 manifest (no cross_size_restore_supported) rejects cross-size."""
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(world_size=4, cross_size_restore_supported=None)
            runtime = _make_runtime(
                Path(tmpdir), world_size=8, allow_world_size_change=True,
            )
            with self.assertRaises(RuntimeError) as ctx:
                _validate_restore_manifest(manifest, runtime)
            self.assertIn("cross-size restore not supported", str(ctx.exception))

    def test_runtime_mode_mismatch_raises(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(runtime_mode="FSDP")
            runtime = _make_runtime(Path(tmpdir), runtime_mode="DDP")
            with self.assertRaises(RuntimeError) as ctx:
                _validate_restore_manifest(manifest, runtime)
            self.assertIn("runtimeMode", str(ctx.exception))

    def test_gpu_shape_mismatch_raises(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(gpu_shape="8xA100")
            runtime = _make_runtime(Path(tmpdir), gpu_shape="8xH100")
            with self.assertRaises(RuntimeError) as ctx:
                _validate_restore_manifest(manifest, runtime)
            self.assertIn("gpuShape", str(ctx.exception))

    def test_multiple_mismatches_all_reported(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = _make_manifest(
                cluster_identity="cluster-a",
                runtime_mode="FSDP",
            )
            runtime = _make_runtime(
                Path(tmpdir), cluster_identity="cluster-b", runtime_mode="DDP",
            )
            with self.assertRaises(RuntimeError) as ctx:
                _validate_restore_manifest(manifest, runtime)
            msg = str(ctx.exception)
            self.assertIn("clusterIdentity", msg)
            self.assertIn("runtimeMode", msg)


if __name__ == "__main__":
    unittest.main()
