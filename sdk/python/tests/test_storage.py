from __future__ import annotations

import io
import tempfile
import unittest
from pathlib import Path

from yield_sdk.storage import S3Storage, S3StorageConfig, parse_s3_uri


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
        payload = data.read(length)
        self.objects[(bucket_name, object_name)] = payload

    def get_object(self, bucket_name: str, object_name: str):
        return _FakeResponse(self.objects[(bucket_name, object_name)])

    def stat_object(self, bucket_name: str, object_name: str):
        return _FakeStat(len(self.objects[(bucket_name, object_name)]))


class StorageTests(unittest.TestCase):
    def test_parse_s3_uri(self) -> None:
        parsed = parse_s3_uri("s3://bucket/checkpoints/ckpt-1/metadata/runtime.json")
        self.assertEqual(parsed.bucket, "bucket")
        self.assertEqual(parsed.key, "checkpoints/ckpt-1/metadata/runtime.json")

    def test_upload_and_download_bytes_and_files(self) -> None:
        storage = S3Storage(
            S3StorageConfig(endpoint="minio.example:9000", access_key="minio", secret_key="miniopass"),
            client=_FakeClient(),
            auto_create_bucket=True,
        )

        upload_uri = "s3://phase1/demo/payload.txt"
        storage.upload_bytes(b"hello", upload_uri, content_type="text/plain")
        self.assertEqual(storage.download_bytes(upload_uri), b"hello")

        with tempfile.TemporaryDirectory() as tmpdir:
            source_path = Path(tmpdir) / "source.txt"
            target_path = Path(tmpdir) / "downloaded.txt"
            source_path.write_text("world", encoding="utf-8")
            storage.upload_file(source_path, "s3://phase1/demo/source.txt")
            storage.download_file("s3://phase1/demo/source.txt", target_path)
            self.assertEqual(target_path.read_text(encoding="utf-8"), "world")
            self.assertEqual(storage.stat("s3://phase1/demo/source.txt").size, 5)


if __name__ == "__main__":
    unittest.main()
