from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from yield_sdk.control import ControlFile, ControlFileError, PAUSED, RUNNING, load_control_record


class ControlTests(unittest.TestCase):
    def test_missing_control_file_defaults_to_running(self) -> None:
        record = load_control_record("/tmp/does-not-exist-yield-sdk.json")
        self.assertEqual(record.desired_state, RUNNING)
        self.assertFalse(record.yield_requested)

    def test_control_file_detects_pause_request(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({"desiredState": PAUSED, "requestId": "pause-001"}), encoding="utf-8")

            watcher = ControlFile(control_path)
            record = watcher.read()

            self.assertEqual(record.desired_state, PAUSED)
            self.assertEqual(record.request_id, "pause-001")
            self.assertTrue(watcher.yield_requested())

    def test_invalid_desired_state_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({"desiredState": "StopNow"}), encoding="utf-8")

            with self.assertRaises(ControlFileError):
                load_control_record(control_path)


class Phase9ElasticControlTests(unittest.TestCase):
    """Tests for Phase 9 elasticity fields in the control file."""

    def test_target_worker_count_parsed_from_control_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({
                "desiredState": "Running",
                "targetWorkerCount": 4,
                "resizeRequestId": "resize-001",
            }), encoding="utf-8")

            record = load_control_record(control_path)
            self.assertEqual(record.desired_state, RUNNING)
            self.assertEqual(record.target_worker_count, 4)
            self.assertEqual(record.resize_request_id, "resize-001")
            self.assertTrue(record.resize_requested)

    def test_target_worker_count_snake_case_parsed(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({
                "desiredState": "Running",
                "target_worker_count": 2,
                "resize_request_id": "resize-002",
            }), encoding="utf-8")

            record = load_control_record(control_path)
            self.assertEqual(record.target_worker_count, 2)
            self.assertEqual(record.resize_request_id, "resize-002")

    def test_missing_target_worker_count_is_none(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({"desiredState": "Running"}), encoding="utf-8")

            record = load_control_record(control_path)
            self.assertIsNone(record.target_worker_count)
            self.assertFalse(record.resize_requested)

    def test_target_worker_count_does_not_leak_into_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({
                "desiredState": "Running",
                "targetWorkerCount": 4,
                "resizeRequestId": "resize-003",
                "customField": "hello",
            }), encoding="utf-8")

            record = load_control_record(control_path)
            self.assertEqual(record.target_worker_count, 4)
            self.assertIsNotNone(record.metadata)
            self.assertEqual(record.metadata["customField"], "hello")
            self.assertNotIn("targetWorkerCount", record.metadata)
            self.assertNotIn("resizeRequestId", record.metadata)

    def test_pause_with_target_worker_count(self) -> None:
        """A Paused state with a target worker count is valid — yield takes priority."""
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({
                "desiredState": "Paused",
                "targetWorkerCount": 2,
                "requestId": "pause-002",
            }), encoding="utf-8")

            record = load_control_record(control_path)
            self.assertTrue(record.yield_requested)
            self.assertEqual(record.target_worker_count, 2)
            self.assertTrue(record.resize_requested)

    def test_backward_compat_plain_running_no_elastic_fields(self) -> None:
        """A Phase 1-8 control file without elastic fields still works."""
        with tempfile.TemporaryDirectory() as tmpdir:
            control_path = Path(tmpdir) / "control.json"
            control_path.write_text(json.dumps({
                "desiredState": "Running",
                "requestId": "req-legacy",
                "updatedAt": "2026-01-01T00:00:00Z",
            }), encoding="utf-8")

            record = load_control_record(control_path)
            self.assertEqual(record.desired_state, RUNNING)
            self.assertEqual(record.request_id, "req-legacy")
            self.assertIsNone(record.target_worker_count)
            self.assertIsNone(record.resize_request_id)
            self.assertFalse(record.resize_requested)
            self.assertFalse(record.yield_requested)

    def test_none_path_produces_default_record(self) -> None:
        record = load_control_record(None)
        self.assertIsNone(record.target_worker_count)
        self.assertFalse(record.resize_requested)


if __name__ == "__main__":
    unittest.main()
