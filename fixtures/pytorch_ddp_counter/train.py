from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path
from typing import Any

from yield_sdk.checkpoint import restore_checkpoint, save_checkpoint
from yield_sdk.control import ControlFile
from yield_sdk.elastic import (
    ElasticConfig,
    ElasticityMode,
    ResizeDirection,
    build_resize_checkpoint_context,
    evaluate_resize,
    write_resize_signal,
)
from yield_sdk.runtime import RuntimeConfig, choose_backend
from yield_sdk.storage import S3Storage


def _import_torch():
    try:
        import torch
        import torch.distributed as dist
        import torch.nn as nn
        from torch.nn.parallel import DistributedDataParallel as DDP
    except ModuleNotFoundError as exc:
        raise SystemExit("torch is required to run the pytorch_ddp_counter fixture") from exc
    return torch, dist, nn, DDP


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="DDP counter trainer with manual yield and elastic resize support.")
    parser.add_argument("--max-steps", type=int, default=24)
    parser.add_argument("--batch-size", type=int, default=16)
    parser.add_argument("--input-dim", type=int, default=8)
    parser.add_argument("--hidden-dim", type=int, default=16)
    parser.add_argument("--lr", type=float, default=0.01)
    parser.add_argument("--seed", type=int, default=1337)
    parser.add_argument("--checkpoint-every", type=int, default=0)
    parser.add_argument("--sleep-per-step", type=float, default=0.0)
    parser.add_argument("--backend", default=None)
    parser.add_argument("--control-file", default=os.environ.get("YIELD_SDK_CONTROL_FILE"))
    parser.add_argument("--progress-file", default=os.environ.get("YIELD_SDK_PROGRESS_FILE"))
    # Phase 9: elastic resize knobs.
    parser.add_argument("--shrink-barrier-timeout", type=float, default=30.0,
                        help="Timeout in seconds for the cooperative shrink barrier.")
    parser.add_argument("--warmup-steps", type=int, default=0,
                        help="Steps to skip before checking for resize requests.")
    parser.add_argument("--resize-check-every", type=int, default=1,
                        help="Check for resize requests every N steps.")
    parser.add_argument("--resize-signal-dir", default=os.environ.get("YIELD_SDK_RESIZE_SIGNAL_DIR"),
                        help="Directory to write resize signal files.")
    return parser.parse_args()


def init_distributed(backend: str):
    torch, dist, _, _ = _import_torch()

    if dist.is_available() and not dist.is_initialized() and "RANK" in os.environ and "WORLD_SIZE" in os.environ:
        if backend == "nccl":
            local_rank = int(os.environ.get("LOCAL_RANK", "0"))
            torch.cuda.set_device(local_rank)
        dist.init_process_group(backend=backend, init_method="env://")

    if dist.is_available() and dist.is_initialized():
        return dist.get_rank(), dist.get_world_size()
    return 0, 1


def barrier() -> None:
    _, dist, _, _ = _import_torch()
    if dist.is_available() and dist.is_initialized():
        dist.barrier()


def destroy_distributed() -> None:
    _, dist, _, _ = _import_torch()
    if dist.is_available() and dist.is_initialized():
        dist.destroy_process_group()


def build_model(input_dim: int, hidden_dim: int):
    _, _, nn, _ = _import_torch()
    return nn.Sequential(
        nn.Linear(input_dim, hidden_dim),
        nn.Tanh(),
        nn.Linear(hidden_dim, 1),
    )


def make_batch(step: int, batch_size: int, input_dim: int, device, rank: int):
    torch, _, _, _ = _import_torch()
    base = torch.arange(batch_size * input_dim, dtype=torch.float32, device=device).reshape(batch_size, input_dim)
    x = torch.sin(base * 0.05 + float(step) * 0.1 + float(rank))
    target = torch.cos(x.sum(dim=1, keepdim=True) * 0.25)
    return x, target


def write_json(path: str | Path | None, payload: dict[str, Any]) -> None:
    if not path:
        return
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _build_elastic_config(
    runtime: RuntimeConfig,
    world_size: int,
    control_file: ControlFile,
    shrink_barrier_timeout: float,
) -> ElasticConfig:
    """Build the elastic config from runtime + control file state."""
    mode_raw = runtime.elasticity_mode
    try:
        mode = ElasticityMode(mode_raw)
    except ValueError:
        mode = ElasticityMode.DISABLED

    if mode == ElasticityMode.DISABLED:
        return ElasticConfig(
            mode=ElasticityMode.DISABLED,
            current_worker_count=world_size,
            target_worker_count=world_size,
        )

    # Target comes from control file first, then runtime config, then current.
    target = world_size
    if runtime.target_worker_count is not None:
        target = runtime.target_worker_count
    record = control_file.read()
    if record.target_worker_count is not None:
        target = record.target_worker_count

    return ElasticConfig(
        mode=mode,
        current_worker_count=world_size,
        target_worker_count=target,
        supports_in_place_shrink=runtime.supports_in_place_shrink,
        shrink_barrier_timeout_seconds=shrink_barrier_timeout,
    )


def main() -> int:
    args = parse_args()
    runtime = RuntimeConfig.from_env()
    if args.control_file:
        runtime = RuntimeConfig(
            cluster_identity=runtime.cluster_identity,
            rtj_identity=runtime.rtj_identity,
            run_attempt=runtime.run_attempt,
            runtime_mode=runtime.runtime_mode,
            world_size=runtime.world_size,
            gpu_shape=runtime.gpu_shape,
            image_identity=runtime.image_identity,
            code_version_identity=runtime.code_version_identity,
            optimizer_mode=runtime.optimizer_mode,
            sharding_mode=runtime.sharding_mode,
            checkpoint_storage_uri=runtime.checkpoint_storage_uri,
            staging_root=runtime.staging_root,
            restore_root=runtime.restore_root,
            control_file=Path(args.control_file),
            restore_manifest_uri=runtime.restore_manifest_uri,
            yield_marker_path=runtime.yield_marker_path,
            yield_marker_uri=runtime.yield_marker_uri,
            elasticity_mode=runtime.elasticity_mode,
            target_worker_count=runtime.target_worker_count,
            supports_in_place_shrink=runtime.supports_in_place_shrink,
        )

    backend = choose_backend(args.backend)
    rank, world_size = init_distributed(backend)
    if runtime.world_size != world_size:
        runtime = RuntimeConfig(
            cluster_identity=runtime.cluster_identity,
            rtj_identity=runtime.rtj_identity,
            run_attempt=runtime.run_attempt,
            runtime_mode=runtime.runtime_mode,
            world_size=world_size,
            gpu_shape=runtime.gpu_shape,
            image_identity=runtime.image_identity,
            code_version_identity=runtime.code_version_identity,
            optimizer_mode=runtime.optimizer_mode,
            sharding_mode=runtime.sharding_mode,
            checkpoint_storage_uri=runtime.checkpoint_storage_uri,
            staging_root=runtime.staging_root,
            restore_root=runtime.restore_root,
            control_file=runtime.control_file,
            restore_manifest_uri=runtime.restore_manifest_uri,
            yield_marker_path=runtime.yield_marker_path,
            yield_marker_uri=runtime.yield_marker_uri,
            elasticity_mode=runtime.elasticity_mode,
            target_worker_count=runtime.target_worker_count,
            supports_in_place_shrink=runtime.supports_in_place_shrink,
        )

    torch, _, nn, DDP = _import_torch()
    if backend == "nccl" and torch.cuda.is_available():
        local_rank = int(os.environ.get("LOCAL_RANK", "0"))
        device = torch.device("cuda", local_rank)
    else:
        device = torch.device("cpu")

    torch.manual_seed(args.seed + rank)
    if torch.cuda.is_available():
        torch.cuda.manual_seed_all(args.seed + rank)

    model = build_model(args.input_dim, args.hidden_dim).to(device)
    optimizer = torch.optim.AdamW(model.parameters(), lr=args.lr)
    wrapped_model = model
    if world_size > 1:
        if device.type == "cuda":
            wrapped_model = DDP(model, device_ids=[device.index], output_device=device.index)
        else:
            wrapped_model = DDP(model)

    storage = S3Storage.from_env()
    control = ControlFile(runtime.control_file)

    start_step = 0
    restored_checkpoint_id = None
    restore_mode = None
    if runtime.restore_manifest_uri:
        restore_result = restore_checkpoint(
            model=wrapped_model,
            optimizer=optimizer,
            runtime=runtime,
            storage=storage,
            manifest_uri=runtime.restore_manifest_uri,
        )
        start_step = restore_result.step
        restored_checkpoint_id = restore_result.manifest.checkpoint_id
        restore_mode = restore_result.restore_mode

    loss_fn = nn.MSELoss()
    last_loss = 0.0

    if rank == 0:
        print(
            json.dumps(
                {
                    "event": "trainer_start",
                    "backend": backend,
                    "device": str(device),
                    "world_size": world_size,
                    "restored_checkpoint_id": restored_checkpoint_id,
                    "start_step": start_step,
                    "restore_mode": restore_mode,
                    "elasticity_mode": runtime.elasticity_mode,
                    "target_worker_count": runtime.target_worker_count,
                }
            ),
            flush=True,
        )

    for step in range(start_step + 1, args.max_steps + 1):
        optimizer.zero_grad(set_to_none=True)
        inputs, targets = make_batch(step, args.batch_size, args.input_dim, device, rank)
        predictions = wrapped_model(inputs)
        loss = loss_fn(predictions, targets)
        loss.backward()
        optimizer.step()
        last_loss = float(loss.detach().cpu().item())

        if rank == 0:
            write_json(
                args.progress_file,
                {
                    "step": step,
                    "loss": last_loss,
                    "backend": backend,
                    "worldSize": world_size,
                    "restoredCheckpointID": restored_checkpoint_id,
                },
            )

        if args.sleep_per_step > 0:
            time.sleep(args.sleep_per_step)

        if args.checkpoint_every > 0 and step % args.checkpoint_every == 0:
            checkpoint_result = save_checkpoint(
                model=wrapped_model,
                optimizer=optimizer,
                runtime=runtime,
                storage=storage,
                step=step,
                trainer_state={"last_loss": last_loss, "yielded": False},
            )
            barrier()
            if rank == 0:
                print(
                    json.dumps(
                        {
                            "event": "periodic_checkpoint",
                            "checkpointID": checkpoint_result.manifest.checkpoint_id,
                            "manifestURI": checkpoint_result.manifest.manifest_uri,
                            "step": step,
                        }
                    ),
                    flush=True,
                )

        # Phase 9: check for elastic resize requests.
        if (
            step > start_step + args.warmup_steps
            and step % args.resize_check_every == 0
        ):
            elastic_config = _build_elastic_config(
                runtime, world_size, control, args.shrink_barrier_timeout,
            )
            if elastic_config.resize_requested:
                resize_outcome = evaluate_resize(elastic_config)
                resize_ctx = build_resize_checkpoint_context(elastic_config)

                barrier()
                checkpoint_result = save_checkpoint(
                    model=wrapped_model,
                    optimizer=optimizer,
                    runtime=runtime,
                    storage=storage,
                    step=step,
                    trainer_state={
                        "last_loss": last_loss,
                        "yielded": True,
                        "resize_direction": elastic_config.resize_direction.value,
                        "resize_target": elastic_config.target_worker_count,
                    },
                )
                barrier()

                # Patch resize metadata into manifest if context is available.
                if resize_ctx is not None:
                    manifest = checkpoint_result.manifest
                    manifest.resize_active_worker_count = resize_ctx.active_worker_count
                    manifest.resize_target_worker_count = resize_ctx.target_worker_count
                    manifest.resize_direction = resize_ctx.resize_direction.value
                    manifest.resize_reason = resize_ctx.resize_reason
                    manifest.resize_in_place_shrink_supported = resize_ctx.in_place_shrink_supported
                    # Re-upload manifest with resize metadata.
                    if rank == 0:
                        manifest_json = manifest.to_json()
                        storage.upload_bytes(
                            manifest_json.encode("utf-8"),
                            manifest.manifest_uri,
                            content_type="application/json",
                        )

                if rank == 0:
                    # Write resize signal for the controller.
                    signal_dir = Path(args.resize_signal_dir) if args.resize_signal_dir else None
                    if signal_dir:
                        write_resize_signal(
                            signal_dir,
                            resize_outcome,
                            checkpoint_id=checkpoint_result.manifest.checkpoint_id,
                            manifest_uri=checkpoint_result.manifest.manifest_uri,
                        )

                    print(
                        json.dumps(
                            {
                                "event": "resize_checkpoint",
                                "direction": elastic_config.resize_direction.value,
                                "currentWorkerCount": elastic_config.current_worker_count,
                                "targetWorkerCount": elastic_config.target_worker_count,
                                "outcome": resize_outcome.outcome.value,
                                "requiresCheckpoint": resize_outcome.requires_checkpoint,
                                "inPlaceShrinkSupported": resize_outcome.in_place_shrink_supported,
                                "checkpointID": checkpoint_result.manifest.checkpoint_id,
                                "manifestURI": checkpoint_result.manifest.manifest_uri,
                                "step": step,
                            }
                        ),
                        flush=True,
                    )

                destroy_distributed()
                return 0

        # Phase 1: check for manual yield (pause) request.
        control_record = control.read()
        if control_record.yield_requested:
            barrier()
            checkpoint_result = save_checkpoint(
                model=wrapped_model,
                optimizer=optimizer,
                runtime=runtime,
                storage=storage,
                step=step,
                trainer_state={"last_loss": last_loss, "yielded": True},
            )
            barrier()
            if rank == 0:
                marker_payload = {
                    "checkpointID": checkpoint_result.manifest.checkpoint_id,
                    "manifestURI": checkpoint_result.manifest.manifest_uri,
                    "globalStep": checkpoint_result.manifest.global_step,
                    "requestID": control_record.request_id,
                    "completionTimestamp": checkpoint_result.manifest.completion_timestamp,
                }
                write_json(runtime.yield_marker_path, marker_payload)
                if runtime.yield_marker_uri:
                    storage.upload_bytes(
                        (json.dumps(marker_payload, indent=2, sort_keys=True) + "\n").encode("utf-8"),
                        runtime.yield_marker_uri,
                        content_type="application/json",
                    )
                print(
                    json.dumps(
                        {
                            "event": "yield_complete",
                            "checkpointID": checkpoint_result.manifest.checkpoint_id,
                            "manifestURI": checkpoint_result.manifest.manifest_uri,
                            "step": step,
                            "requestID": control_record.request_id,
                        }
                    ),
                    flush=True,
                )
            destroy_distributed()
            return 0

    if rank == 0:
        print(
            json.dumps(
                {
                    "event": "training_complete",
                    "finalStep": args.max_steps,
                    "lastLoss": last_loss,
                }
            ),
            flush=True,
        )
    destroy_distributed()
    return 0


if __name__ == "__main__":
    sys.exit(main())
