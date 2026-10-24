# Salad BzMiner Worker

Self-starting NVIDIA GPU mining worker for local Docker and SaladCloud Container Groups. It runs the official BzMiner v100.50 release binary, mines PEARL by default, optionally merge-mines NOCK, generates a unique worker name per replica, supervises the miner with signal-safe restart/backoff, and exposes read-only health and Prometheus metrics.

The wallet in the defaults is a public payout address. This project never asks for or stores a private key, seed phrase, or signing credential.

## Status

- Linux amd64 image built from pinned `ubuntu:24.04` and the official BzMiner v100.50 release archive (SHA-256 verified). The upstream `bzminer/bzminer` image was not pullable from the build environment (Docker Hub 404, GHCR 401), so this repository builds an equivalent runtime from the official binary.
- Unit tests, image checks, no-GPU failure path, fake-miner integration, restart/backoff, graceful SIGTERM, and a local real-GPU smoke all pass on the development host.
- Real-GPU smoke on 2× NVIDIA GRID P4-8Q (8 GiB): two CUDA devices detected, pool authorization and connection succeeded, `/readyz` returned ready, and BzMiner reported 0 accepted shares and 0 hashrate at the ~10-second observation point. That is not evidence of earnings; Pearl on Pascal is slow and shares are difficulty-dependent.
- **Published image:** `ghcr.io/timmy-time/salad-bzminer-worker:1.0.0` (also `:1.0` and `:sha-79b61b1`), digest `sha256:737b0b1fb26f2296b3a42f4c30cf861cde666dc41b0c3197be7e77e8fc783be6`. Anonymous pull verified.
- No paid SaladCloud replicas were deployed. See [docs/SALAD_DEPLOYMENT.md](docs/SALAD_DEPLOYMENT.md) before deploying.

## Architecture

```
container PID 1: /app/worker run
  ├─ config/defaults.env + process environment -> strict validation
  ├─ NVIDIA GPU detection (nvidia-smi -> bzminer --gpu-info -> /proc fallback)
  ├─ worker identity (Salad instance ID + hostname + GPU, 8-hex suffix)
  ├─ HTTP server on HEALTH_PORT (0.0.0.0:8080)
  │    ├─ GET /healthz   process liveness
  │    ├─ GET /readyz    miner + API readiness (never share-based)
  │    └─ GET /metrics   Prometheus text
  ├─ BzMiner child on 127.0.0.1:4014 (API for stats; never published)
  └─ supervisor: SIGTERM/SIGINT forwarding, process-group kill, capped jittered backoff
```

The Go supervisor is intentionally small and stdlib-only. A shell entrypoint would add no value: the Go binary is PID 1, forwards signals, reaps the child, and is the entrypoint wrapper. BzMiner itself is a child, not PID 1.

## Quick start (local Docker)

```bash
docker build -t salad-bzminer-worker:1.0.0 .

docker run -d \
  --name salad-bzminer \
  --gpus all \
  --restart unless-stopped \
  -p 8080:8080 \
  -e MINING_ALGORITHM=pearl \
  -e MINING_POOL=stratum+tcp://pearl-au.luckypool.io:3360 \
  -e WORKER_PREFIX=salad \
  salad-bzminer-worker:1.0.0

docker logs -f salad-bzminer
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
curl -s localhost:8080/metrics | grep bzminer_
```

The image supplies the default PEARL wallet and pool; `MINING_WALLET` and `MINING_POOL` override them.

### Docker Compose

```bash
cp .env.example .env
# edit .env if needed
docker compose up -d --build
docker compose logs -f
```

Compose reserves all NVIDIA GPUs, publishes only `8080`, and uses `init: true` plus `stop_grace_period: 30s`.

## PEARL defaults

| Setting | Value |
|---|---|
| Algorithm | `pearl` |
| Pool | `stratum+tcp://pearl-au.luckypool.io:3360` |
| Wallet | `prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90` |
| Worker prefix | `salad` |

The resulting pool login is `wallet.worker`, for example `prl1…fc90.salad-rtx4090-1a2b3c4d`.

Runtime self-updates and plugin downloads are disabled (`--auto-update off`, `--plugin-update off`), so the image stays pinned to v100.50.

## PEARL + NOCK merge mining

LuckyPool's documented format appends the native Nockchain address to the Pearl wallet:

```bash
-e NOCK_WALLET=<native-base58-nock-address>
```


The worker passes `PRL_WALLET+NOCK_WALLET` to BzMiner, which sends it as `PRL_WALLET+NOCK_WALLET.WORKER`. Do not use a wrapped `0x…` NOCK token address; the pool cannot pay NOCK on Nockchain to it. No second miner and no CPU mining are started.

## Configuration

All variables, defaults, validation, precedence, extra-argument grammar, and the verified BzMiner flag mapping are in [docs/CONFIGURATION.md](docs/CONFIGURATION.md). Common ones:

| Variable | Default |
|---|---|
| `MINING_ALGORITHM` | `pearl` |
| `MINING_POOL` | `stratum+tcp://pearl-au.luckypool.io:3360` |
| `MINING_WALLET` | public PRL address above |
| `WORKER_PREFIX` | `salad` |
| `WORKER_NAME` | generated |
| `GPU_ONLY` | `true` |
| `CPU_MINING` | `false` |
| `NOCK_WALLET` | empty |
| `MINER_EXTRA_ARGS` | empty |
| `LOG_LEVEL` | `info` |
| `HEALTH_PORT` | `8080` |
| `HEALTH_ENABLED` | `true` |

Inspect the resolved command without launching the miner:

```bash
docker run --rm --gpus all salad-bzminer-worker:1.0.0 print-command
```

## Monitoring and health

- `/healthz` is liveness: 200 while the supervisor is alive, 503 while stopping/fatal. It does not claim the miner works.
- `/readyz` is mining readiness: 200 only when the miner process is running, GPUs were detected, and BzMiner's JSON API is fresh (or, when the API is disabled, after 30 seconds). A dead miner is never ready. Accepted shares are deliberately not required.
- `/metrics` exposes process, GPU, pool, share, and hashrate values; the wallet is never included. BzMiner's own API on `127.0.0.1:4014` has unauthenticated control endpoints and is never published.

Readiness reason strings: `gpu_not_detected`, `miner_not_running`, `miner_starting`, `startup_timeout`, `miner_api_unreachable`.

## SaladCloud Container Groups

Use the image as a normal OCI image, reserve an NVIDIA GPU class, expose HTTP port `8080`, and configure probes as described in [docs/SALAD_DEPLOYMENT.md](docs/SALAD_DEPLOYMENT.md). The worker uses the documented `SALAD_INSTANCE_ID`/`SALAD_MACHINE_ID` variables when present, with `HOSTNAME` (Salad defaults it to the instance ID) as the portable fallback. No fixed hostname, IP, persistent volume, SSH session, or Docker socket is required. Replicas mine independently to the same wallet with distinct generated worker names.

SaladCloud documents containerized mining as a supported workload (its mining guide and miner recipes). Confirm the current customer terms and your project permissions before paying for replicas; Chef mining is not evidence of customer permission.

## Building and publishing

```bash
make image
# or
docker build --build-arg VERSION=1.0.0 \
  --build-arg REVISION="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --tag ghcr.io/timmy-time/salad-bzminer-worker:1.0.0 .
```

`.github/workflows/docker-publish.yml` publishes to `ghcr.io/timmy-time/salad-bzminer-worker` on version tags or an explicit manual run. It uses `GITHUB_TOKEN` with `contents: read` and `packages: write`. The v1.0.0 tag was published successfully.

Docker Hub is optional and manual, after an interactive login:

```bash
docker login
docker tag salad-bzminer-worker:1.0.0 docker.io/<user>/salad-bzminer-worker:1.0.0
docker push docker.io/<user>/salad-bzminer-worker:1.0.0
```

Never commit credentials.

## Upgrading and rollback

- **Worker image:** deploy a new explicit tag or digest; keep the previous tag for rollback. Never use `latest` for production.
- **BzMiner version:** change `BZMINER_VERSION`, `BZMINER_URL`, and `BZMINER_SHA256` in `Dockerfile`, update `BzMinerVersion` in `internal/version/version.go`, rebuild, run `go test -race ./...` and `tests/integration/test-image.sh`, then tag a new worker release.
- **Rollback:** redeploy the previous image tag/digest. Worker identity and configuration are environment-driven, so no local state migration is needed.

## Tests

```bash
go vet ./...
go test -race ./...
./tests/integration/test-image.sh
```

The integration script builds the image, checks the non-root user and labels, verifies invalid-config and no-GPU exit codes, runs a fake BzMiner through readiness/metrics/graceful shutdown, verifies crash restart/backoff, and conditionally runs the real-GPU smoke. It never treats zero shares as a failure.

## Supported GPUs

RTX 3060, 3070, 3080, 3090, 4060, 4070, 4080, 4090, 5060, 5070, 5080, 5090, and compatible NVIDIA CUDA GPUs, subject to BzMiner v100.50 and host driver support. Pearl on Pascal (local GRID P4 test hardware) starts and connects but is not a performance recommendation. The container does not install kernel drivers; the NVIDIA Container Toolkit injects host libraries.

## Security

No privileged mode, no Docker socket, non-root UID 10001, no runtime package installation, loopback-only miner API, read-only management endpoints, public wallet only, digest-pinned base, checksum-pinned miner binary, stdlib-only Go. See [docs/SECURITY.md](docs/SECURITY.md).

## License

MIT. BzMiner is third-party software distributed under its own license; it is downloaded from the official release with a verified checksum.
