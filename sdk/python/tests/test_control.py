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


if __name__ == "__main__":
    unittest.main()
