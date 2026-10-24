# SaladCloud Container Group deployment

SaladCloud runs each replica on an interruptible node. This worker is designed for that lifecycle: configuration comes from environment variables, identity is derived per instance, the miner is supervised, and no local volume or SSH session is needed.

## Before deploying

1. Confirm that customer-deployed cryptocurrency mining is permitted for your organization under the current SaladCloud terms. SaladCloud publishes a mining guide and miner recipes, but that is not a substitute for checking your agreement and project permissions. Chef mining is a different product.
2. Build and push the image to a registry SaladCloud can pull from. GHCR package visibility must be public, or configure registry authentication in the Container Group.
3. Do not use `latest` in production. Use an explicit worker version tag or digest.
4. Use a public payout wallet. Never put a private key or seed phrase in environment variables.

## Container Group settings

| Field | Value |
|---|---|
| Name | `salad-bzminer-worker` (lowercase DNS-style) |
| Display name | `Salad BzMiner Worker` |
| Image | `ghcr.io/timmy-time/salad-bzminer-worker:1.0.0` (or digest) |
| Replicas | 1 for validation, then as many as your quota allows |
| GPU class | An NVIDIA class from `sce-gpu-availability`/Portal; use its `gpu_classes` UUID |
| vCPU | 2 |
| Memory | 4096 MB |
| Storage | 10 GiB (`10737418240` bytes) |
| `/dev/shm` | 64 MB (default; Pearl CUDA does not need a large shm) |
| Image caching | enabled |
| Command override | **none**; the image `CMD` is `run` |
| Autostart | enabled |
| Restart policy | `always` |
| HTTP port / Container Gateway | `8080`, protocol `http` |
| Authentication | off (the endpoints are read-only; do not publish BzMiner's `4014`) |
| Startup probe | HTTP GET `/healthz`, port 8080 |
| Liveness probe | HTTP GET `/healthz`, port 8080 |
| Readiness probe | HTTP GET `/readyz`, port 8080 |
| Persistent volume | none |
| Outbound network | TCP 3360 to the pool (and 3361/3362 if you switch LuckyPool difficulty ports); DNS |

### Probe timing

These values tolerate GPU enumeration and BzMiner's first API response without restarting a healthy worker:

| Probe | Initial delay | Period | Timeout | Success | Failure |
|---|---|---|---|---|---|
| Startup (`/healthz`) | 10 s | 10 s | 5 s | 1 | 18 |
| Liveness (`/healthz`) | 0 s | 30 s | 5 s | 1 | 3 |
| Readiness (`/readyz`) | 0 s | 30 s | 5 s | 1 | 3 |

Semantics:

- A failed startup or liveness probe reallocates the container to another node. `/healthz` is process liveness; it returns 200 while the supervisor is alive, including during a child restart/backoff.
- A failed readiness probe does not kill the container; it removes it from the Container Gateway. `/readyz` is 200 only when the miner process is running, GPUs were detected, and BzMiner's API is fresh. It does not require an accepted share.
- Do not use `/readyz` as a liveness probe. A slow pool or a high-difficulty Pearl share gap must not reallocate the node.
- Docker's image `HEALTHCHECK` uses `/healthz` for the same reason.

### Environment variables

Set at minimum:

```text
MINING_WALLET=prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90
WORKER_PREFIX=salad
```

Optional:

```text
MINING_POOL=stratum+tcp://pearl-au.luckypool.io:3360
MINING_POOL_FALLBACK=
NOCK_WALLET=
LOG_LEVEL=info
MINER_EXTRA_ARGS=
```

Do not set `SALAD_INSTANCE_ID`, `SALAD_MACHINE_ID`, `HOSTNAME`, `HEALTH_PORT`, or `BZMINER_BIN`; Salad injects its identity variables and the image defaults are correct.

The image does not set `NVIDIA_VISIBLE_DEVICES`; the runtime must expose the allocated NVIDIA devices (Docker `--gpus` and SaladCloud GPU allocation do). Do not override it with `all` on a shared node.

## Exact Container Group create request

The following JSON uses the current SaladCloud Container Group API field names. Replace `<gpu-class-uuid>` with an NVIDIA class UUID from the Portal or `GET /api/public/organizations/<org>/availability/sce-gpu-availability`. Replace the image and wallet.

```json
{
  "name": "salad-bzminer-worker",
  "display_name": "Salad BzMiner Worker",
  "replicas": 1,
  "autostart_policy": true,
  "restart_policy": "always",
  "container": {
    "image": "ghcr.io/timmy-time/salad-bzminer-worker:1.0.0",
    "image_caching": true,
    "resources": {
      "cpu": 2,
      "memory": 4096,
      "storage_amount": 10737418240,
      "shm_size": 64,
      "gpu_classes": ["<gpu-class-uuid>"]
    },
    "environment_variables": {
      "MINING_WALLET": "prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90",
      "WORKER_PREFIX": "salad",
      "LOG_LEVEL": "info"
    }
  },
  "networking": {
    "auth": false,
    "port": 8080,
    "protocol": "http",
    "load_balancer": "round_robin"
  },
  "startup_probe": {
    "http": {
      "headers": [{"name": "User-Agent", "value": "salad-health-probe"}],
      "path": "/healthz",
      "port": 8080,
      "scheme": "http"
    },
    "initial_delay_seconds": 10,
    "period_seconds": 10,
    "timeout_seconds": 5,
    "success_threshold": 1,
    "failure_threshold": 18
  },
  "liveness_probe": {
    "http": {
      "headers": [{"name": "User-Agent", "value": "salad-health-probe"}],
      "path": "/healthz",
      "port": 8080,
      "scheme": "http"
    },
    "initial_delay_seconds": 0,
    "period_seconds": 30,
    "timeout_seconds": 5,
    "success_threshold": 1,
    "failure_threshold": 3
  },
  "readiness_probe": {
    "http": {
      "headers": [{"name": "User-Agent", "value": "salad-health-probe"}],
      "path": "/readyz",
      "port": 8080,
      "scheme": "http"
    },
    "initial_delay_seconds": 0,
    "period_seconds": 30,
    "timeout_seconds": 5,
    "success_threshold": 1,
    "failure_threshold": 3
  }
}
```

Submit it with the public API, for example:

```bash
curl -X POST \
  "https://api.salad.com/api/public/organizations/$SALAD_ORGANIZATION/projects/$SALAD_PROJECT/containers" \
  -H "Salad-Api-Key: $SALAD_API_KEY" \
  -H "Content-Type: application/json" \
  --data @container-group.json
```

Keep `SALAD_API_KEY` in a secret store; never commit it. Verify the created group and instance state by reading the API back, not by assuming the POST succeeded.

## Scaling and worker names

Each replica derives its own name from `SALAD_INSTANCE_ID`, hostname, and GPU identity. All replicas can use the same wallet; the pool sees distinct `wallet.worker` logins. A reallocation creates a new instance ID and therefore a new worker name; that is expected for an ephemeral workload. Do not use a fixed explicit `WORKER_NAME` across replicas.

There is no shared state, no persistent disk, and no fixed hostname/IP. The Container Gateway port exists only for health/metrics; mining is outbound stratum traffic.

## Verifying a deployment

In the Portal:

1. Instance state is running and the readiness check is green.
2. Logs show `health server listening`, `GPU detected`, `miner started`, and BzMiner `pool authorized`/`connected`.
3. Open the gateway URL and request `/readyz` and `/metrics`; confirm `bzminer_gpu_count` and `bzminer_pool_connected`.
4. On the pool dashboard, confirm the worker name appears and, eventually, accepted shares. Pearl shares are heavy; absence in the first minutes is not a failure.

## Interruptions and restarts

Salad nodes are interruptible. On stop/reallocation, the supervisor forwards SIGTERM, BzMiner submits in-flight work where possible, and the container exits 0. Salad starts a fresh replica elsewhere. No cleanup or manual action is needed.

Inside a replica, if BzMiner exits unexpectedly, the supervisor restarts it with 2 s → 300 s capped exponential backoff and jitter. It never restarts merely because shares are absent. A missing GPU or invalid configuration exits 1 before the restart loop starts; fix the configuration or GPU class instead of relying on external restarts.
