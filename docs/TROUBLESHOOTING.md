# Troubleshooting

## First checks

```bash
docker logs -f salad-bzminer
docker inspect --format '{{.State.ExitCode}} {{.State.Health.Status}}' salad-bzminer
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
curl -s localhost:8080/metrics | grep -E 'bzminer_(gpu|miner|pool|shares|hashrate)'
docker run --rm --gpus all --entrypoint /opt/bzminer/bzminer salad-bzminer-worker:1.0.0 --gpu-info
```

On SaladCloud, read the instance logs in the Portal or export them to external logging. `print-command` shows the exact argv and generated worker name without mining:

```bash
docker run --rm --gpus all salad-bzminer-worker:1.0.0 print-command
```

## “no NVIDIA GPU detected”

The worker exits 1 before starting BzMiner. The message includes `NVIDIA_VISIBLE_DEVICES` and hints.

- Local Docker: add `--gpus all`; verify `docker run --rm --gpus all nvidia/cuda:12.2.0-base-ubuntu22.04 nvidia-smi`.
- SaladCloud: reserve an NVIDIA GPU class, not a CPU-only class. Confirm the instance actually has a GPU.
- `NVIDIA_VISIBLE_DEVICES=none` or `void` disables the device; remove it.
- The container needs the NVIDIA Container Toolkit on the host; it cannot install kernel drivers. Do not try to install a driver in the image.
- If `nvidia-smi` is missing but CUDA works, the worker falls back to `bzminer --gpu-info`; if that also reports zero, CUDA does not see a device.
- The last-resort `/proc/driver/nvidia` path is only used when no explicit visibility filter is set; it is not trusted to enumerate host GPUs under `NVIDIA_VISIBLE_DEVICES=0`-style filters.

## CUDA / driver mismatch

Symptoms: BzMiner exits during CUDA initialization, `/readyz` stays 503, or `--gpu-info` reports a driver API error.

- Check the host driver and CUDA version with `nvidia-smi`.
- The image contains the BzMiner v100.50 release binary, not NVIDIA userspace libraries. The toolkit injects the host's `libcuda.so.1`/`libnvidia-ml.so.1`; a pinned copy would break on driver updates.
- `NVIDIA_DRIVER_CAPABILITIES` is set to `compute,utility`; `utility` is required for NVML telemetry.
- On SaladCloud you cannot change the host driver. Choose a GPU class/node with a driver supported by BzMiner v100.50, or pin a BzMiner release that supports the available driver.
- Do not run the container privileged to “fix” CUDA. Mining does not need it.

## Pool connection failures

BzMiner logs `connecting`, `authorized`, `connected`, or a pool error. The supervisor does not restart on a pool failure; BzMiner reconnects internally.

- Verify DNS and outbound TCP 3360 (`pearl-au.luckypool.io:3360`). For TLS pools use `stratum+ssl://` on the pool's TLS port and make sure `ca-certificates` is present (it is in the image).
- Check that the wallet/worker login is accepted. The pool login is `wallet.worker`; a merge login is `PRL+NOCK.worker`.
- Try `MINING_POOL_FALLBACK` with a second URL.
- A residential Salad node may have blocked or unstable outbound connectivity; check the node's logs and reallocate if needed.
- `bzminer_pool_connected` comes from BzMiner's `/api/snapshot`, not from a share being accepted.

## `/readyz` is 503

The response contains an ordered `reasons` array:

| Reason | Meaning |
|---|---|
| `gpu_not_detected` | GPU enumeration failed; see above. |
| `miner_not_running` | The child is dead or between restart attempts. Check logs for the exit code/signal. |
| `miner_starting` | The child is running but BzMiner's API has not answered yet (API mode), or the process is <30 s old (no-API mode). |
| `startup_timeout` | No API response after `STARTUP_TIMEOUT_SECONDS`; the miner is not killed, but readiness stays false. Check BzMiner logs and the API port. |
| `miner_api_unreachable` | The last successful API scrape is more than 30 s old. |

`/healthz` can still be 200 while `/readyz` is 503: liveness is not mining readiness. Never use `/readyz` as a liveness probe.

## Restart loop

- Invalid configuration or a missing GPU exits 1 immediately; the container runtime may restart it externally. Fix the environment or GPU class rather than extending probes.
- A BzMiner crash restarts with 2 s, 4 s, 8 s … up to 300 s and ±20% jitter. Logs show `miner exited` and `restarting miner`.
- Repeated immediate crashes usually mean a bad binary, an unsupported GPU/driver, or a corrupted image. Run `/opt/bzminer/bzminer --gpu-info` and check `pool authorized`.
- The worker deliberately disables BzMiner's share-based stall guard (`stall_guard.enabled=false`). Do not re-enable it via `MINER_EXTRA_ARGS` on slow hardware; a long Pearl share gap is not a process failure.

## Zero hashrate or zero accepted shares

Pearl is a zk proof-of-work: shares are expensive and hashrate numbers are small. On a GRID P4/Pascal test card, the local smoke connected and started mining but reported zero shares in the first seconds. That is expected, not proof of a fault.

- Check `bzminer_hashrate_total_hs`, `bzminer_shares_accepted_total`, `bzminer_pool_connected`, and BzMiner's `tbs`/difficulty in the logs.
- Do not judge a rig in the first minutes. Use the pool dashboard with the worker name.
- Verify the GPU is actually mining in `/api/snapshot` (`devices[].mining`, `status: Mining`).
- If hashrate stays exactly zero for many minutes and BzMiner logs no CUDA errors, confirm the algorithm and pool match (`pearl` on a Pearl pool).

## Worker name problems

- Generated names are `prefix-model-8hex`; the suffix comes from Salad's instance ID/hostname/GPU. A new instance gets a new name.
- If you set `WORKER_NAME`, it is sanitized and used verbatim (no suffix). Do not use one explicit name on multiple replicas.
- Pool dashboards may show `wallet.worker`; the wallet and worker are separated by a dot.

## NOCK merge mining

- Use a native Nockchain base58 address, not a `0x` wrapped-token address; the worker rejects `0x`.
- Merge mining requires `MINING_ALGORITHM=pearl`.
- The login becomes `PRL+NOCK.worker`. Confirm the pool supports the merge and check NOCK stats at the pool's Nock address page.
- If the miner version does not support merge mining, PEARL still mines normally; the worker does not start a second process.

## Image pull / registry

- GHCR packages are private by default. Make the package public or configure registry authentication in the Container Group.
- SaladCloud must be able to pull the image; check the instance pulling state and events.
- Use an explicit version tag or digest, not `latest`.

## Upgrades and rollback

- Deploy a new explicit tag/digest; keep the prior tag.
- Roll back by redeploying the previous image tag/digest. Configuration is environment-driven; there is no local state to migrate.
- For a BzMiner upgrade, change the URL/version/SHA-256 in the Dockerfile, update `internal/version/version.go`, and rerun the full test suite before tagging.
