from __future__ import annotations

import io
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol
from urllib.parse import urlparse


class StorageError(RuntimeError):
    """Raised when an object-store operation fails."""


@dataclass(frozen=True)
class S3URI:
    bucket: str
    key: str

    def __str__(self) -> str:
        if self.key:
            return f"s3://{self.bucket}/{self.key}"
        return f"s3://{self.bucket}"

    def join(self, *parts: str) -> "S3URI":
        segments = [segment.strip("/") for segment in [self.key, *parts] if segment and segment.strip("/")]
        return S3URI(bucket=self.bucket, key="/".join(segments))


def parse_s3_uri(uri: str) -> S3URI:
    parsed = urlparse(uri)
    if parsed.scheme != "s3":
        raise StorageError(f"unsupported storage URI {uri!r}; only s3:// URIs are supported")
    if not parsed.netloc:
        raise StorageError(f"storage URI {uri!r} is missing a bucket name")
    return S3URI(bucket=parsed.netloc, key=parsed.path.lstrip("/"))


@dataclass(frozen=True)
class S3StorageConfig:
    endpoint: str
    access_key: str
    secret_key: str
    secure: bool = False
    region: str | None = None
    session_token: str | None = None

    @classmethod
    def from_env(cls) -> "S3StorageConfig":
        endpoint = os.environ.get("YIELD_SDK_S3_ENDPOINT") or os.environ.get("S3_ENDPOINT") or os.environ.get(
            "AWS_ENDPOINT_URL"
        )
        access_key = (
            os.environ.get("YIELD_SDK_S3_ACCESS_KEY")
            or os.environ.get("AWS_ACCESS_KEY_ID")
            or os.environ.get("MINIO_ROOT_USER")
        )
        secret_key = (
            os.environ.get("YIELD_SDK_S3_SECRET_KEY")
            or os.environ.get("AWS_SECRET_ACCESS_KEY")
            or os.environ.get("MINIO_ROOT_PASSWORD")
        )
        session_token = os.environ.get("YIELD_SDK_S3_SESSION_TOKEN") or os.environ.get("AWS_SESSION_TOKEN")
        region = os.environ.get("YIELD_SDK_S3_REGION") or os.environ.get("AWS_REGION")
        secure_value = os.environ.get("YIELD_SDK_S3_SECURE", os.environ.get("S3_SECURE", "false")).strip().lower()
        secure = secure_value in {"1", "true", "yes", "on"}

        if not endpoint or not access_key or not secret_key:
            raise StorageError(
                "missing S3 configuration; set YIELD_SDK_S3_ENDPOINT, "
                "YIELD_SDK_S3_ACCESS_KEY, and YIELD_SDK_S3_SECRET_KEY"
            )

        if endpoint.startswith("http://"):
            endpoint = endpoint[len("http://") :]
        elif endpoint.startswith("https://"):
            endpoint = endpoint[len("https://") :]
            secure = True

        return cls(
            endpoint=endpoint,
            access_key=access_key,
            secret_key=secret_key,
            secure=secure,
            region=region,
            session_token=session_token,
        )


class _S3ClientProtocol(Protocol):
    def fput_object(self, bucket_name: str, object_name: str, file_path: str, content_type: str | None = None): ...
    def fget_object(self, bucket_name: str, object_name: str, file_path: str): ...
    def put_object(
        self,
        bucket_name: str,
        object_name: str,
        data,
        length: int,
        content_type: str | None = None,
    ): ...
    def get_object(self, bucket_name: str, object_name: str): ...
    def stat_object(self, bucket_name: str, object_name: str): ...
    def bucket_exists(self, bucket_name: str) -> bool: ...
    def make_bucket(self, bucket_name: str): ...


class S3Storage:
    def __init__(
        self,
        config: S3StorageConfig,
        *,
        client: _S3ClientProtocol | None = None,
        auto_create_bucket: bool = False,
    ):
        self.config = config
        self._client = client
        self.auto_create_bucket = auto_create_bucket
        self._known_buckets: set[str] = set()

    @classmethod
    def from_env(cls, *, client: _S3ClientProtocol | None = None, auto_create_bucket: bool = False) -> "S3Storage":
        return cls(S3StorageConfig.from_env(), client=client, auto_create_bucket=auto_create_bucket)

    @property
    def client(self) -> _S3ClientProtocol:
        if self._client is None:
            try:
                from minio import Minio
            except ModuleNotFoundError as exc:
                raise StorageError("minio is required for S3-compatible storage operations") from exc
            self._client = Minio(
                self.config.endpoint,
                access_key=self.config.access_key,
                secret_key=self.config.secret_key,
                session_token=self.config.session_token,
                secure=self.config.secure,
                region=self.config.region,
            )
        return self._client

    def _ensure_bucket(self, bucket: str) -> None:
        if not self.auto_create_bucket or bucket in self._known_buckets:
            return
        if not self.client.bucket_exists(bucket):
            self.client.make_bucket(bucket)
        self._known_buckets.add(bucket)

    def upload_file(self, local_path: str | Path, destination_uri: str, *, content_type: str | None = None) -> str:
        target = parse_s3_uri(destination_uri)
        self._ensure_bucket(target.bucket)
        self.client.fput_object(target.bucket, target.key, str(local_path), content_type=content_type)
        return str(target)

    def upload_bytes(self, payload: bytes, destination_uri: str, *, content_type: str | None = None) -> str:
        target = parse_s3_uri(destination_uri)
        self._ensure_bucket(target.bucket)
        stream = io.BytesIO(payload)
        self.client.put_object(target.bucket, target.key, stream, len(payload), content_type=content_type)
        return str(target)

    def download_file(self, source_uri: str, local_path: str | Path) -> Path:
        source = parse_s3_uri(source_uri)
        target_path = Path(local_path)
        target_path.parent.mkdir(parents=True, exist_ok=True)
        self.client.fget_object(source.bucket, source.key, str(target_path))
        return target_path

    def download_bytes(self, source_uri: str) -> bytes:
        source = parse_s3_uri(source_uri)
        response = self.client.get_object(source.bucket, source.key)
        try:
            return response.read()
        finally:
            close = getattr(response, "close", None)
            release_conn = getattr(response, "release_conn", None)
            if callable(close):
                close()
            if callable(release_conn):
                release_conn()

    def stat(self, source_uri: str):
        source = parse_s3_uri(source_uri)
        return self.client.stat_object(source.bucket, source.key)
