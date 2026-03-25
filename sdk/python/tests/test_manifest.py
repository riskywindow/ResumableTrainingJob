from __future__ import annotations

import json
import unittest

from yield_sdk.manifest import (
    CHECKPOINT_FORMAT_DCP_V1,
    ArtifactEntry,
    CheckpointManifest,
    ManifestValidationError,
)


def _base_artifact() -> ArtifactEntry:
    return ArtifactEntry(
        name="metadata-runtime.json",
        relative_path="metadata/runtime.json",
        object_uri="s3://bucket/demo-rtj/checkpoints/ckpt-1/metadata/runtime.json",
        size_bytes=512,
        digest_value="a" * 64,
    )


def _base_manifest(**overrides) -> CheckpointManifest:
    defaults = dict(
        checkpoint_id="ckpt-20260321T010203Z-a1-s4",
        cluster_identity="kind-phase1",
        rtj_identity="demo-rtj",
        run_attempt=1,
        global_step=4,
        wall_clock_timestamp="2026-03-21T01:02:03Z",
        image_identity="local/fixture:dev",
        code_version_identity="git:abc123",
        runtime_mode="DDP",
        world_size=2,
        gpu_shape="cpu",
        optimizer_mode="adamw",
        sharding_mode="replicated-optimizer-state",
        producer_version="0.1.0",
        storage_root_uri="s3://bucket/demo-rtj/checkpoints/ckpt-20260321T010203Z-a1-s4",
        completion_timestamp="2026-03-21T01:02:05Z",
        manifest_uri="s3://bucket/demo-rtj/manifests/ckpt-20260321T010203Z-a1-s4.manifest.json",
        artifacts=[_base_artifact()],
    )
    defaults.update(overrides)
    return CheckpointManifest(**defaults)


class ManifestTests(unittest.TestCase):
    def test_round_trip_manifest_is_complete(self) -> None:
        manifest = _base_manifest()
        encoded = manifest.to_json()
        decoded = CheckpointManifest.from_json(encoded)

        self.assertEqual(decoded.checkpoint_id, manifest.checkpoint_id)
        self.assertEqual(decoded.manifest_uri, manifest.manifest_uri)
        self.assertEqual(decoded.artifacts[0].relative_path, "metadata/runtime.json")

    def test_manifest_requires_completion_timestamp(self) -> None:
        manifest = _base_manifest(completion_timestamp=None)
        with self.assertRaises(ManifestValidationError):
            manifest.to_dict()


class Phase3ManifestFieldsTests(unittest.TestCase):
    def test_phase3_fields_round_trip(self) -> None:
        manifest = _base_manifest(
            leader_count=0,
            worker_count=2,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=True,
        )
        encoded = manifest.to_json()
        decoded = CheckpointManifest.from_json(encoded)

        self.assertEqual(decoded.leader_count, 0)
        self.assertEqual(decoded.worker_count, 2)
        self.assertEqual(decoded.checkpoint_format_version, CHECKPOINT_FORMAT_DCP_V1)
        self.assertTrue(decoded.cross_size_restore_supported)

    def test_phase2_manifest_without_phase3_fields_decodes(self) -> None:
        """A Phase 2 manifest with no Phase 3 fields should decode with None defaults."""
        manifest = _base_manifest()
        payload = manifest.to_dict()
        # Simulate Phase 2: remove Phase 3 keys
        payload.pop("leaderCount", None)
        payload.pop("workerCount", None)
        payload.pop("checkpointFormatVersion", None)
        payload.pop("crossSizeRestoreSupported", None)
        raw = json.dumps(payload, indent=2, sort_keys=True)

        decoded = CheckpointManifest.from_json(raw)
        self.assertIsNone(decoded.leader_count)
        self.assertIsNone(decoded.worker_count)
        self.assertIsNone(decoded.checkpoint_format_version)
        self.assertIsNone(decoded.cross_size_restore_supported)

    def test_phase3_fields_in_serialized_json(self) -> None:
        manifest = _base_manifest(
            leader_count=1,
            worker_count=7,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=False,
        )
        payload = manifest.to_dict()

        self.assertEqual(payload["leaderCount"], 1)
        self.assertEqual(payload["workerCount"], 7)
        self.assertEqual(payload["checkpointFormatVersion"], "dcp/v1")
        self.assertFalse(payload["crossSizeRestoreSupported"])

    def test_optional_phase3_fields_omitted_when_none(self) -> None:
        manifest = _base_manifest()
        payload = manifest.to_dict()

        self.assertNotIn("leaderCount", payload)
        self.assertNotIn("workerCount", payload)
        self.assertNotIn("checkpointFormatVersion", payload)
        self.assertNotIn("crossSizeRestoreSupported", payload)

    def test_cross_size_restore_false_is_serialized(self) -> None:
        manifest = _base_manifest(cross_size_restore_supported=False)
        payload = manifest.to_dict()
        self.assertIn("crossSizeRestoreSupported", payload)
        self.assertFalse(payload["crossSizeRestoreSupported"])

    def test_manifest_completeness_with_all_fields(self) -> None:
        """Manifest with all Phase 3 fields validates successfully."""
        manifest = _base_manifest(
            leader_count=0,
            worker_count=8,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=True,
            replica_count=8,
            rank_layout=[{"rank": i, "localRank": i} for i in range(8)],
            dcp_metadata={"backend": "torch.distributed.checkpoint"},
        )
        manifest.validate()
        payload = manifest.to_dict()

        required_keys = {
            "checkpointID", "clusterIdentity", "rtjIdentity", "runAttempt",
            "globalStep", "wallClockTimestamp", "imageIdentity",
            "codeVersionIdentity", "runtimeMode", "worldSize", "gpuShape",
            "optimizerMode", "shardingMode", "formatVersion", "producerVersion",
            "storageRootURI", "artifacts", "completionTimestamp",
        }
        phase3_keys = {
            "leaderCount", "workerCount", "checkpointFormatVersion",
            "crossSizeRestoreSupported",
        }
        for key in required_keys | phase3_keys:
            self.assertIn(key, payload, f"missing key: {key}")


if __name__ == "__main__":
    unittest.main()
