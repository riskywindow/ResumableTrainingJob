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


class Phase8DeviceProfileTests(unittest.TestCase):
    def test_device_profile_fingerprint_round_trip(self) -> None:
        manifest = _base_manifest(
            device_profile_fingerprint="abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
        )
        encoded = manifest.to_json()
        decoded = CheckpointManifest.from_json(encoded)
        self.assertEqual(
            decoded.device_profile_fingerprint,
            "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
        )

    def test_device_profile_fingerprint_in_serialized_json(self) -> None:
        manifest = _base_manifest(
            device_profile_fingerprint="fp123",
        )
        payload = manifest.to_dict()
        self.assertEqual(payload["deviceProfileFingerprint"], "fp123")

    def test_device_profile_fingerprint_omitted_when_none(self) -> None:
        manifest = _base_manifest()
        payload = manifest.to_dict()
        self.assertNotIn("deviceProfileFingerprint", payload)

    def test_phase7_manifest_without_device_profile_decodes(self) -> None:
        """A Phase 7 manifest with no device profile field decodes with None default."""
        manifest = _base_manifest()
        payload = manifest.to_dict()
        # Simulate Phase 7: ensure no device profile key.
        payload.pop("deviceProfileFingerprint", None)
        raw = json.dumps(payload, indent=2, sort_keys=True)

        decoded = CheckpointManifest.from_json(raw)
        self.assertIsNone(decoded.device_profile_fingerprint)

    def test_device_profile_with_phase3_fields_coexist(self) -> None:
        """Phase 8 device profile and Phase 3 fields can coexist."""
        manifest = _base_manifest(
            leader_count=0,
            worker_count=4,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=True,
            device_profile_fingerprint="sha256-device-fp",
        )
        payload = manifest.to_dict()
        self.assertIn("crossSizeRestoreSupported", payload)
        self.assertIn("deviceProfileFingerprint", payload)
        self.assertEqual(payload["deviceProfileFingerprint"], "sha256-device-fp")
        self.assertTrue(payload["crossSizeRestoreSupported"])

    def test_manifest_completeness_with_all_phase8_fields(self) -> None:
        """Manifest with all Phase 3 + Phase 8 fields validates successfully."""
        manifest = _base_manifest(
            leader_count=0,
            worker_count=8,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=True,
            device_profile_fingerprint="full-fingerprint-value",
        )
        manifest.validate()
        payload = manifest.to_dict()

        phase8_keys = {"deviceProfileFingerprint"}
        for key in phase8_keys:
            self.assertIn(key, payload, f"missing key: {key}")


class Phase9ElasticityMetadataTests(unittest.TestCase):
    """Tests for Phase 9 elasticity metadata in checkpoint manifests."""

    def test_resize_fields_round_trip(self) -> None:
        manifest = _base_manifest(
            resize_active_worker_count=8,
            resize_target_worker_count=4,
            resize_direction="Shrink",
            resize_reason="manual shrink from 8 to 4",
            resize_in_place_shrink_supported=False,
        )
        encoded = manifest.to_json()
        decoded = CheckpointManifest.from_json(encoded)

        self.assertEqual(decoded.resize_active_worker_count, 8)
        self.assertEqual(decoded.resize_target_worker_count, 4)
        self.assertEqual(decoded.resize_direction, "Shrink")
        self.assertEqual(decoded.resize_reason, "manual shrink from 8 to 4")
        self.assertFalse(decoded.resize_in_place_shrink_supported)

    def test_resize_fields_in_serialized_json(self) -> None:
        manifest = _base_manifest(
            resize_active_worker_count=4,
            resize_target_worker_count=8,
            resize_direction="Grow",
            resize_reason="operator scale-up",
            resize_in_place_shrink_supported=False,
        )
        payload = manifest.to_dict()

        self.assertEqual(payload["resizeActiveWorkerCount"], 4)
        self.assertEqual(payload["resizeTargetWorkerCount"], 8)
        self.assertEqual(payload["resizeDirection"], "Grow")
        self.assertEqual(payload["resizeReason"], "operator scale-up")
        self.assertFalse(payload["resizeInPlaceShrinkSupported"])

    def test_resize_fields_omitted_when_none(self) -> None:
        manifest = _base_manifest()
        payload = manifest.to_dict()

        self.assertNotIn("resizeActiveWorkerCount", payload)
        self.assertNotIn("resizeTargetWorkerCount", payload)
        self.assertNotIn("resizeDirection", payload)
        self.assertNotIn("resizeReason", payload)
        self.assertNotIn("resizeInPlaceShrinkSupported", payload)

    def test_phase8_manifest_without_phase9_fields_decodes(self) -> None:
        """A Phase 8 manifest with no Phase 9 fields should decode with None defaults."""
        manifest = _base_manifest(
            device_profile_fingerprint="phase8-fp",
        )
        payload = manifest.to_dict()
        # Simulate Phase 8: ensure no resize keys.
        for key in ("resizeActiveWorkerCount", "resizeTargetWorkerCount",
                     "resizeDirection", "resizeReason", "resizeInPlaceShrinkSupported"):
            payload.pop(key, None)
        raw = json.dumps(payload, indent=2, sort_keys=True)

        decoded = CheckpointManifest.from_json(raw)
        self.assertIsNone(decoded.resize_active_worker_count)
        self.assertIsNone(decoded.resize_target_worker_count)
        self.assertIsNone(decoded.resize_direction)
        self.assertIsNone(decoded.resize_reason)
        self.assertIsNone(decoded.resize_in_place_shrink_supported)
        # Phase 8 field preserved.
        self.assertEqual(decoded.device_profile_fingerprint, "phase8-fp")

    def test_resize_fields_coexist_with_phase3_and_phase8(self) -> None:
        """Phase 9 resize fields coexist with Phase 3 and Phase 8 fields."""
        manifest = _base_manifest(
            leader_count=0,
            worker_count=8,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=True,
            device_profile_fingerprint="fp-all-phases",
            resize_active_worker_count=8,
            resize_target_worker_count=4,
            resize_direction="Shrink",
            resize_reason="shrink test",
            resize_in_place_shrink_supported=True,
        )
        payload = manifest.to_dict()

        # Phase 3 fields.
        self.assertIn("crossSizeRestoreSupported", payload)
        # Phase 8 fields.
        self.assertIn("deviceProfileFingerprint", payload)
        # Phase 9 fields.
        self.assertIn("resizeActiveWorkerCount", payload)
        self.assertIn("resizeDirection", payload)

    def test_resize_in_place_shrink_false_is_serialized(self) -> None:
        manifest = _base_manifest(resize_in_place_shrink_supported=False)
        payload = manifest.to_dict()
        self.assertIn("resizeInPlaceShrinkSupported", payload)
        self.assertFalse(payload["resizeInPlaceShrinkSupported"])

    def test_manifest_completeness_with_all_phase9_fields(self) -> None:
        """Manifest with all phases' fields validates successfully."""
        manifest = _base_manifest(
            leader_count=0,
            worker_count=8,
            checkpoint_format_version=CHECKPOINT_FORMAT_DCP_V1,
            cross_size_restore_supported=True,
            device_profile_fingerprint="full-fp",
            resize_active_worker_count=8,
            resize_target_worker_count=4,
            resize_direction="Shrink",
            resize_reason="full test",
            resize_in_place_shrink_supported=False,
        )
        manifest.validate()
        payload = manifest.to_dict()

        phase9_keys = {
            "resizeActiveWorkerCount", "resizeTargetWorkerCount",
            "resizeDirection", "resizeReason", "resizeInPlaceShrinkSupported",
        }
        for key in phase9_keys:
            self.assertIn(key, payload, f"missing key: {key}")


if __name__ == "__main__":
    unittest.main()
