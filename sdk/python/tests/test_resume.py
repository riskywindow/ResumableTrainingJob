from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import torch

from yield_sdk.checkpoint import (
    RESTORE_MODE_RESHARD,
    RESTORE_MODE_SAME_SIZE,
    restore_checkpoint,
    save_checkpoint,
)
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

    def put_object(
        self,
        bucket_name: str,
        object_name: str,
        data,
        length: int,
        content_type: str | None = None,
    ):
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


def _make_runtime(base: Path, *, world_size: int = 1, run_attempt: int = 1,
                   restore_manifest_uri: str | None = None,
                   allow_world_size_change: bool = False,
                   original_world_size: int | None = None) -> RuntimeConfig:
    return RuntimeConfig(
        cluster_identity="phase1-kind",
        rtj_identity="demo-rtj",
        run_attempt=run_attempt,
        runtime_mode="DDP",
        world_size=world_size,
        gpu_shape="cpu",
        image_identity="local/fixture:dev",
        code_version_identity="git:abc123",
        optimizer_mode="adamw",
        sharding_mode="replicated-optimizer-state",
        checkpoint_storage_uri="s3://bucket/demo-rtj",
        staging_root=base / f"staging-a{run_attempt}",
        restore_root=base / f"restore-a{run_attempt}",
        restore_manifest_uri=restore_manifest_uri,
        allow_world_size_change=allow_world_size_change,
        original_world_size=original_world_size,
    )


class SameSizeResumeTests(unittest.TestCase):
    def test_restore_keeps_global_step_monotonic(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            initial_runtime = _make_runtime(base, world_size=1, run_attempt=1)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            save_result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=initial_runtime,
                storage=storage, step=5, trainer_state={"last_loss": 1.25},
            )

            resumed_runtime = _make_runtime(
                base, world_size=1, run_attempt=2,
                restore_manifest_uri=save_result.manifest.manifest_uri,
            )

            resumed_model = torch.nn.Linear(4, 1)
            resumed_optimizer = torch.optim.AdamW(resumed_model.parameters(), lr=0.01)
            restore_result = restore_checkpoint(
                model=resumed_model, optimizer=resumed_optimizer,
                runtime=resumed_runtime, storage=storage,
            )

            self.assertEqual(restore_result.step, 5)
            self.assertEqual(restore_result.restore_mode, RESTORE_MODE_SAME_SIZE)
            self.assertGreater(restore_result.step + 1, save_result.manifest.global_step)

    def test_same_size_restore_mode_is_same_size(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            runtime = _make_runtime(base, world_size=1, run_attempt=1)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            save_result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=runtime,
                storage=storage, step=3,
            )

            restore_runtime = _make_runtime(
                base, world_size=1, run_attempt=2,
                restore_manifest_uri=save_result.manifest.manifest_uri,
            )

            result = restore_checkpoint(
                model=torch.nn.Linear(4, 1),
                optimizer=torch.optim.AdamW(torch.nn.Linear(4, 1).parameters(), lr=0.01),
                runtime=restore_runtime, storage=storage,
            )
            self.assertEqual(result.restore_mode, RESTORE_MODE_SAME_SIZE)


class DifferentSizeResumeTests(unittest.TestCase):
    def test_different_size_restore_with_allow_world_size_change(self) -> None:
        """When allow_world_size_change=True, restoring from a different world size succeeds."""
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()

            # Save at world_size=1
            save_runtime = _make_runtime(base, world_size=1, run_attempt=1)
            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            save_result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=save_runtime,
                storage=storage, step=10, trainer_state={"last_loss": 0.5},
            )

            # Verify the manifest has cross_size_restore_supported=True
            self.assertTrue(save_result.manifest.cross_size_restore_supported)

            # Restore at world_size=2 with allow_world_size_change=True
            # NOTE: In a real distributed setup, DCP handles the resharding.
            # For this unit test with world_size=1 runtime (no dist), we
            # verify the validation and restore_mode logic by restoring at
            # a "declared" world_size=2 but actually running single-process.
            # DCP.load works because the single-process reader can read
            # a single-process checkpoint.
            restore_runtime = _make_runtime(
                base, world_size=2, run_attempt=2,
                restore_manifest_uri=save_result.manifest.manifest_uri,
                allow_world_size_change=True,
                original_world_size=1,
            )

            restored_model = torch.nn.Linear(4, 1)
            restored_optimizer = torch.optim.AdamW(restored_model.parameters(), lr=0.01)
            result = restore_checkpoint(
                model=restored_model, optimizer=restored_optimizer,
                runtime=restore_runtime, storage=storage,
            )

            self.assertEqual(result.step, 10)
            self.assertEqual(result.restore_mode, RESTORE_MODE_RESHARD)
            self.assertEqual(result.manifest.world_size, 1)

    def test_manifest_records_cross_size_support(self) -> None:
        """Phase 3 checkpoints declare cross_size_restore_supported=True."""
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            runtime = _make_runtime(base, world_size=1, run_attempt=1)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=runtime,
                storage=storage, step=1,
            )

            self.assertTrue(result.manifest.cross_size_restore_supported)
            self.assertEqual(result.manifest.checkpoint_format_version, "dcp/v1")
            self.assertEqual(result.manifest.worker_count, 1)
            self.assertEqual(result.manifest.leader_count, 0)


class IncompatibleResumeTests(unittest.TestCase):
    def test_restore_rejects_world_size_mismatch_without_flag(self) -> None:
        """Without allow_world_size_change, world size mismatch is rejected."""
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            save_runtime = _make_runtime(base, world_size=1, run_attempt=1)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            save_result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=save_runtime,
                storage=storage, step=3, trainer_state={"last_loss": 2.0},
            )

            incompatible_runtime = _make_runtime(
                base, world_size=2, run_attempt=2,
                restore_manifest_uri=save_result.manifest.manifest_uri,
                allow_world_size_change=False,
            )

            with self.assertRaises(RuntimeError) as ctx:
                restore_checkpoint(
                    model=torch.nn.Linear(4, 1),
                    optimizer=torch.optim.AdamW(torch.nn.Linear(4, 1).parameters(), lr=0.01),
                    runtime=incompatible_runtime, storage=storage,
                )
            self.assertIn("worldSize", str(ctx.exception))

    def test_restore_rejects_cluster_identity_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            save_runtime = _make_runtime(base, world_size=1, run_attempt=1)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            save_result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=save_runtime,
                storage=storage, step=1,
            )

            bad_runtime = RuntimeConfig(
                cluster_identity="different-cluster",
                rtj_identity="demo-rtj",
                run_attempt=2,
                runtime_mode="DDP",
                world_size=1,
                gpu_shape="cpu",
                image_identity="local/fixture:dev",
                code_version_identity="git:abc123",
                optimizer_mode="adamw",
                sharding_mode="replicated-optimizer-state",
                checkpoint_storage_uri="s3://bucket/demo-rtj",
                staging_root=base / "staging-bad",
                restore_root=base / "restore-bad",
                restore_manifest_uri=save_result.manifest.manifest_uri,
            )

            with self.assertRaises(RuntimeError) as ctx:
                restore_checkpoint(
                    model=torch.nn.Linear(4, 1),
                    optimizer=torch.optim.AdamW(torch.nn.Linear(4, 1).parameters(), lr=0.01),
                    runtime=bad_runtime, storage=storage,
                )
            self.assertIn("clusterIdentity", str(ctx.exception))

    def test_restore_rejects_cross_size_when_manifest_unsupported(self) -> None:
        """Even with allow_world_size_change=True, reject if manifest says unsupported."""
        with tempfile.TemporaryDirectory() as tmpdir:
            base = Path(tmpdir)
            storage = _make_storage()
            save_runtime = _make_runtime(base, world_size=1, run_attempt=1)

            model = torch.nn.Linear(4, 1)
            optimizer = torch.optim.AdamW(model.parameters(), lr=0.01)
            save_result = save_checkpoint(
                model=model, optimizer=optimizer, runtime=save_runtime,
                storage=storage, step=2,
            )

            # Tamper with manifest: set cross_size_restore_supported=False
            import json
            manifest_key = None
            for key, val in storage._client.objects.items():
                if key[1].endswith(".manifest.json"):
                    manifest_key = key
            self.assertIsNotNone(manifest_key)

            raw = json.loads(storage._client.objects[manifest_key])
            raw["crossSizeRestoreSupported"] = False
            storage._client.objects[manifest_key] = json.dumps(raw, indent=2).encode("utf-8")

            restore_runtime = _make_runtime(
                base, world_size=2, run_attempt=2,
                restore_manifest_uri=save_result.manifest.manifest_uri,
                allow_world_size_change=True,
            )

            with self.assertRaises(RuntimeError) as ctx:
                restore_checkpoint(
                    model=torch.nn.Linear(4, 1),
                    optimizer=torch.optim.AdamW(torch.nn.Linear(4, 1).parameters(), lr=0.01),
                    runtime=restore_runtime, storage=storage,
                )
            self.assertIn("cross-size restore not supported", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()
