from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from yield_sdk.elastic import (
    RESIZE_SIGNAL_FILENAME,
    ElasticConfig,
    ElasticityMode,
    ResizeCheckpointContext,
    ResizeDirection,
    ResizeOutcome,
    ShrinkOutcome,
    build_resize_checkpoint_context,
    evaluate_resize,
    read_resize_signal,
    write_resize_signal,
)


class ElasticConfigTests(unittest.TestCase):
    """Tests for ElasticConfig properties and construction."""

    def test_disabled_mode_reports_no_resize(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.DISABLED,
            current_worker_count=4,
            target_worker_count=2,
        )
        self.assertEqual(config.resize_direction, ResizeDirection.NONE)
        self.assertFalse(config.resize_requested)
        self.assertEqual(config.worker_delta, -2)

    def test_manual_mode_detects_shrink(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=2,
        )
        self.assertEqual(config.resize_direction, ResizeDirection.SHRINK)
        self.assertTrue(config.resize_requested)
        self.assertEqual(config.worker_delta, -2)

    def test_manual_mode_detects_grow(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=2,
            target_worker_count=4,
        )
        self.assertEqual(config.resize_direction, ResizeDirection.GROW)
        self.assertTrue(config.resize_requested)
        self.assertEqual(config.worker_delta, 2)

    def test_manual_mode_no_change_reports_none(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=4,
        )
        self.assertEqual(config.resize_direction, ResizeDirection.NONE)
        self.assertFalse(config.resize_requested)
        self.assertEqual(config.worker_delta, 0)

    def test_from_env_defaults_to_disabled(self) -> None:
        import os
        env = {
            "YIELD_SDK_WORLD_SIZE": "2",
        }
        old_env = {}
        # Clear all elasticity env vars.
        for key in ("YIELD_SDK_ELASTICITY_MODE", "YIELD_SDK_TARGET_WORKER_COUNT",
                     "YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK", "YIELD_SDK_SHRINK_BARRIER_TIMEOUT"):
            old_env[key] = os.environ.pop(key, None)
        for key, val in env.items():
            old_env[key] = os.environ.get(key)
            os.environ[key] = val
        try:
            config = ElasticConfig.from_env()
            self.assertEqual(config.mode, ElasticityMode.DISABLED)
            self.assertEqual(config.current_worker_count, 2)
            self.assertEqual(config.target_worker_count, 2)
            self.assertFalse(config.supports_in_place_shrink)
        finally:
            for key, val in old_env.items():
                if val is None:
                    os.environ.pop(key, None)
                else:
                    os.environ[key] = val

    def test_from_env_manual_mode_with_target(self) -> None:
        import os
        env = {
            "YIELD_SDK_ELASTICITY_MODE": "Manual",
            "YIELD_SDK_WORLD_SIZE": "4",
            "YIELD_SDK_TARGET_WORKER_COUNT": "2",
            "YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK": "true",
            "YIELD_SDK_SHRINK_BARRIER_TIMEOUT": "60.0",
        }
        old_env = {}
        for key, val in env.items():
            old_env[key] = os.environ.get(key)
            os.environ[key] = val
        try:
            config = ElasticConfig.from_env()
            self.assertEqual(config.mode, ElasticityMode.MANUAL)
            self.assertEqual(config.current_worker_count, 4)
            self.assertEqual(config.target_worker_count, 2)
            self.assertTrue(config.supports_in_place_shrink)
            self.assertEqual(config.shrink_barrier_timeout_seconds, 60.0)
        finally:
            for key, val in old_env.items():
                if val is None:
                    os.environ.pop(key, None)
                else:
                    os.environ[key] = val

    def test_from_env_override_current_worker_count(self) -> None:
        config = ElasticConfig.from_env(current_worker_count=8)
        self.assertEqual(config.current_worker_count, 8)


class EvaluateResizeTests(unittest.TestCase):
    """Tests for the evaluate_resize() decision function."""

    def test_no_resize_when_disabled(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.DISABLED,
            current_worker_count=4,
            target_worker_count=2,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.direction, ResizeDirection.NONE)
        self.assertEqual(outcome.outcome, ShrinkOutcome.NOT_REQUESTED)
        self.assertFalse(outcome.requires_checkpoint)

    def test_no_resize_when_same_count(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=4,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.direction, ResizeDirection.NONE)
        self.assertFalse(outcome.requires_checkpoint)

    def test_grow_always_requires_checkpoint(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=2,
            target_worker_count=4,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.direction, ResizeDirection.GROW)
        self.assertTrue(outcome.requires_checkpoint)
        self.assertIn("checkpoint-and-relaunch", outcome.fallback_reason)

    def test_shrink_in_place_supported(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=2,
            supports_in_place_shrink=True,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.direction, ResizeDirection.SHRINK)
        self.assertEqual(outcome.outcome, ShrinkOutcome.SUCCESS)
        self.assertTrue(outcome.in_place_shrink_supported)
        self.assertFalse(outcome.requires_checkpoint)

    def test_shrink_fallback_when_not_supported(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=2,
            supports_in_place_shrink=False,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.direction, ResizeDirection.SHRINK)
        self.assertEqual(outcome.outcome, ShrinkOutcome.FALLBACK_REQUIRED)
        self.assertFalse(outcome.in_place_shrink_supported)
        self.assertTrue(outcome.requires_checkpoint)
        self.assertIn("does not support in-place shrink", outcome.fallback_reason)

    def test_grow_reports_correct_worker_counts(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=2,
            target_worker_count=8,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.current_worker_count, 2)
        self.assertEqual(outcome.target_worker_count, 8)

    def test_shrink_fallback_reports_correct_worker_counts(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=8,
            target_worker_count=2,
        )
        outcome = evaluate_resize(config)
        self.assertEqual(outcome.current_worker_count, 8)
        self.assertEqual(outcome.target_worker_count, 2)


class ResizeOutcomeSerializationTests(unittest.TestCase):
    """Tests for ResizeOutcome round-trip serialization."""

    def test_round_trip(self) -> None:
        outcome = ResizeOutcome(
            direction=ResizeDirection.SHRINK,
            outcome=ShrinkOutcome.FALLBACK_REQUIRED,
            current_worker_count=8,
            target_worker_count=4,
            in_place_shrink_supported=False,
            requires_checkpoint=True,
            fallback_reason="test reason",
        )
        payload = outcome.to_dict()
        restored = ResizeOutcome.from_dict(payload)
        self.assertEqual(restored.direction, outcome.direction)
        self.assertEqual(restored.outcome, outcome.outcome)
        self.assertEqual(restored.current_worker_count, outcome.current_worker_count)
        self.assertEqual(restored.target_worker_count, outcome.target_worker_count)
        self.assertEqual(restored.fallback_reason, outcome.fallback_reason)

    def test_no_fallback_reason_when_absent(self) -> None:
        outcome = ResizeOutcome(
            direction=ResizeDirection.NONE,
            outcome=ShrinkOutcome.NOT_REQUESTED,
            current_worker_count=4,
            target_worker_count=4,
            in_place_shrink_supported=False,
            requires_checkpoint=False,
        )
        payload = outcome.to_dict()
        self.assertNotIn("fallbackReason", payload)


class ResizeCheckpointContextTests(unittest.TestCase):
    """Tests for ResizeCheckpointContext."""

    def test_build_context_for_shrink(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=2,
            supports_in_place_shrink=False,
        )
        ctx = build_resize_checkpoint_context(config)
        self.assertIsNotNone(ctx)
        self.assertEqual(ctx.active_worker_count, 4)
        self.assertEqual(ctx.target_worker_count, 2)
        self.assertEqual(ctx.resize_direction, ResizeDirection.SHRINK)
        self.assertIn("shrink", ctx.resize_reason)

    def test_build_context_for_grow(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=2,
            target_worker_count=4,
        )
        ctx = build_resize_checkpoint_context(config, reason="operator requested scale-up")
        self.assertIsNotNone(ctx)
        self.assertEqual(ctx.resize_reason, "operator requested scale-up")
        self.assertEqual(ctx.resize_direction, ResizeDirection.GROW)

    def test_no_context_when_no_resize(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.MANUAL,
            current_worker_count=4,
            target_worker_count=4,
        )
        ctx = build_resize_checkpoint_context(config)
        self.assertIsNone(ctx)

    def test_no_context_when_disabled(self) -> None:
        config = ElasticConfig(
            mode=ElasticityMode.DISABLED,
            current_worker_count=4,
            target_worker_count=2,
        )
        ctx = build_resize_checkpoint_context(config)
        self.assertIsNone(ctx)

    def test_manifest_fields_serialization(self) -> None:
        ctx = ResizeCheckpointContext(
            active_worker_count=8,
            target_worker_count=4,
            resize_direction=ResizeDirection.SHRINK,
            resize_reason="manual shrink",
            in_place_shrink_supported=False,
        )
        fields = ctx.to_manifest_fields()
        self.assertEqual(fields["resizeActiveWorkerCount"], 8)
        self.assertEqual(fields["resizeTargetWorkerCount"], 4)
        self.assertEqual(fields["resizeDirection"], "Shrink")
        self.assertEqual(fields["resizeReason"], "manual shrink")
        self.assertFalse(fields["resizeInPlaceShrinkSupported"])


class ResizeSignalTests(unittest.TestCase):
    """Tests for resize signal file I/O."""

    def test_write_and_read_signal(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            signal_dir = Path(tmpdir) / "signals"
            outcome = ResizeOutcome(
                direction=ResizeDirection.SHRINK,
                outcome=ShrinkOutcome.FALLBACK_REQUIRED,
                current_worker_count=4,
                target_worker_count=2,
                in_place_shrink_supported=False,
                requires_checkpoint=True,
                fallback_reason="DDP limitation",
            )
            write_resize_signal(
                signal_dir, outcome,
                checkpoint_id="ckpt-123",
                manifest_uri="s3://bucket/manifests/ckpt-123.manifest.json",
            )

            signal = read_resize_signal(signal_dir)
            self.assertIsNotNone(signal)
            self.assertEqual(signal["direction"], "Shrink")
            self.assertEqual(signal["outcome"], "FallbackRequired")
            self.assertEqual(signal["checkpointID"], "ckpt-123")
            self.assertEqual(signal["manifestURI"], "s3://bucket/manifests/ckpt-123.manifest.json")
            self.assertTrue(signal["requiresCheckpoint"])

    def test_read_signal_returns_none_when_absent(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            signal = read_resize_signal(Path(tmpdir))
            self.assertIsNone(signal)

    def test_signal_file_is_valid_json(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            signal_dir = Path(tmpdir)
            outcome = ResizeOutcome(
                direction=ResizeDirection.GROW,
                outcome=ShrinkOutcome.NOT_REQUESTED,
                current_worker_count=2,
                target_worker_count=4,
                in_place_shrink_supported=False,
                requires_checkpoint=True,
            )
            signal_path = write_resize_signal(signal_dir, outcome)
            raw = signal_path.read_text(encoding="utf-8")
            parsed = json.loads(raw)
            self.assertEqual(parsed["direction"], "Grow")

    def test_signal_without_checkpoint_refs(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            signal_dir = Path(tmpdir)
            outcome = ResizeOutcome(
                direction=ResizeDirection.SHRINK,
                outcome=ShrinkOutcome.SUCCESS,
                current_worker_count=4,
                target_worker_count=2,
                in_place_shrink_supported=True,
                requires_checkpoint=False,
            )
            write_resize_signal(signal_dir, outcome)
            signal = read_resize_signal(signal_dir)
            self.assertNotIn("checkpointID", signal)
            self.assertNotIn("manifestURI", signal)


class ElasticDisabledBackwardCompatTests(unittest.TestCase):
    """Tests proving Phase 8 behavior is preserved when elasticity is disabled."""

    def test_disabled_config_never_requests_resize(self) -> None:
        for target in [1, 2, 4, 8, 16]:
            config = ElasticConfig(
                mode=ElasticityMode.DISABLED,
                current_worker_count=4,
                target_worker_count=target,
            )
            self.assertFalse(config.resize_requested)
            outcome = evaluate_resize(config)
            self.assertEqual(outcome.direction, ResizeDirection.NONE)
            self.assertFalse(outcome.requires_checkpoint)

    def test_disabled_config_default_values(self) -> None:
        config = ElasticConfig()
        self.assertEqual(config.mode, ElasticityMode.DISABLED)
        self.assertEqual(config.current_worker_count, 0)
        self.assertEqual(config.target_worker_count, 0)
        self.assertFalse(config.supports_in_place_shrink)
        self.assertFalse(config.resize_requested)


if __name__ == "__main__":
    unittest.main()
