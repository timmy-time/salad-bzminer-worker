# Configuration

Defaults live in [`config/defaults.env`](../config/defaults.env) and are baked into the image at `/app/config/defaults.env`. Precedence:

1. Process environment (even an empty value overrides the file).
2. `/app/config/defaults.env` (or `DEFAULTS_FILE`).
3. Built-in fallback only where noted.

Unknown environment variables are ignored. The wallet is a public payout address; never provide a private key or seed phrase.

## Variable reference

| Variable | Default | Validation / behavior |
|---|---|---|
| `MINING_ALGORITHM` | `pearl` | `^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`; passed to `-a`. |
| `MINING_POOL` | `stratum+tcp://pearl-au.luckypool.io:3360` | `stratum+tcp` or `stratum+ssl`; host and port required; no path, query, fragment, or userinfo. |
| `MINING_POOL_FALLBACK` | empty | Optional second pool URL, same validation. Passed as a second `-p`; BzMiner rotates to it after a disconnect. Single URL only. |
| `MINING_WALLET` | `prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90` | `pearl`: `^prl1[0-9a-z]{40,80}$`; other algorithms: 16–128 chars of `[A-Za-z0-9._:-]`. No whitespace, no `+`. |
| `NOCK_WALLET` | empty | Native Nockchain base58: `^[1-9A-HJ-NP-Za-km-z]{20,128}$`; `0x…` is rejected; only with `pearl`. Composes `MINING_WALLET+NOCK_WALLET`. |
| `WORKER_PREFIX` | `salad` | Sanitized to lowercase `[a-z0-9-]`; must not become empty. |
| `WORKER_NAME` | empty | Empty generates `prefix-model-8hex`. If set, sanitized and used exactly (no suffix); set the same value on multiple replicas only if you accept a pool-side collision. |
| `GPU_ONLY` | `true` | Strict boolean (`true/false/1/0/yes/no/on/off`). |
| `CPU_MINING` | `false` | Strict boolean. Effective CPU mining is `CPU_MINING && !GPU_ONLY`. `GPU_ONLY=true` with `CPU_MINING=true` is an error. |
| `MINER_EXTRA_ARGS` | empty | Shell-like tokens, no shell. Max 64 tokens / 4096 bytes. See grammar and deny list below. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`; also passed to BzMiner as `--log-level`. |
| `HEALTH_ENABLED` | `true` | Enables the worker HTTP server and BzMiner's loopback JSON API. When false, BzMiner HTTP is disabled too. |
| `HEALTH_PORT` | `8080` | 1–65535; must differ from `MINER_HTTP_PORT`. |
| `HEALTH_BIND` | `0.0.0.0` | Valid IP. Use `127.0.0.1` only if an external process needs local-only access. |
| `MINER_HTTP_PORT` | `4014` | 1–65535; BzMiner API port, bound to `127.0.0.1`. Do not publish it. |
| `BZMINER_BIN` | `/opt/bzminer/bzminer` | Must exist and be executable at `run` time. `print-config`/`print-command` skip this check. |
| `NVIDIA_SMI_PATH` | empty | Optional explicit `nvidia-smi` path. Empty uses `PATH`, then `bzminer --gpu-info`, then `/proc/driver/nvidia`. |
| `RESTART_BACKOFF_BASE_SECONDS` | `2` | 1–300. |
| `RESTART_BACKOFF_MAX_SECONDS` | `300` | >= base, <= 3600. |
| `STARTUP_TIMEOUT_SECONDS` | `180` | 10–1800; after this without a successful BzMiner API scrape, `/readyz` reports `startup_timeout`. The miner is not killed for this. |
| `SHUTDOWN_GRACE_SECONDS` | `20` | 1–300; SIGTERM, then SIGKILL to the process group. |
| `DEFAULTS_FILE` | `/app/config/defaults.env` | Advanced; path to the defaults file. |

## CPU semantics

The default workload is GPU-only. The worker always passes `--nvidia --cpu 0` in that case. If `GPU_ONLY=false` and `CPU_MINING=true`, it passes `--nvidia --cpu` (NVIDIA plus CPU) and no `--cpu 0`. It never starts a second miner. BzMiner's own Pearl share prover uses CPU threads as needed even with CPU mining off; that is normal support work.

## Worker identity

Generated name: `<sanitized prefix>-<GPU model slug>-<8 hex>`.

- The suffix is the first 8 hex chars of SHA-256 over the documented Salad identity values present (`SALAD_INSTANCE_ID`, `SALAD_MACHINE_ID`, `SALAD_CONTAINER_GROUP_ID`), hostname, and the first GPU's UUID/PCI ID. Salad documents `HOSTNAME` as defaulting to the instance ID. The name is computed once per process start; supervisor restarts reuse it.
- The model slug strips `NVIDIA`, `GeForce`, and `Tesla` prefixes, removes non-alphanumerics, and is capped at 16 chars: `NVIDIA GeForce RTX 4090` → `rtx4090`; `GRID P4-8Q` → `gridp48q`.
- The total is capped at 32 characters with the suffix preserved.
- Example: `salad-rtx4090-1a2b3c4d`.
- Multiple replicas to the same wallet are safe with generated names because `SALAD_INSTANCE_ID` distinguishes them.

`print-command` shows the generated name and exact argv without starting the miner.

## Extra arguments

`MINER_EXTRA_ARGS` is tokenized by an internal parser, never a shell:

- whitespace separates tokens;
- single quotes are literal;
- double quotes allow `\"` and `\\`;
- a backslash escapes the next character outside single quotes;
- `$`, `;`, `&`, `|`, backticks, and `$(...)` are ordinary characters and remain a single argv element;
- NUL, CR, and LF are rejected; at most 64 tokens and 4096 bytes.

Flags that the worker manages are rejected so environment configuration stays authoritative. This includes `-a`/`--algorithm`, `-w`/`--wallet`, `-p`/`--pool`, `--worker`, `--cpu`/`--cpuN`, `--nvidia`, `--amd`, `--intel`, `--igpu`, `--devices*`, `-o`/`--output`, `--set`, `--config`, `--auto-update`, `--plugin-*`, and `--http_*`/`--api_*`. BzMiner run modes (`--dmon`, `--gpu-info`, `--bench`, `--print-config`, `--version`, …) are also rejected. `--cpu_threads` and `--cpu_affinity` are allowed. The error is `MINER_EXTRA_ARGS: <flag> is managed by the worker`.

Examples:

```bash
-e MINER_EXTRA_ARGS="--intensity 64 --oc-fan-speed 't:70[30-90]'"
```

## Verified BzMiner v100.50 flags

The command is built from flags present in the v100.50 `--help` output:

| Purpose | Flag(s) | Notes |
|---|---|---|
| algorithm | `-a <algo>` | `pearl`/`pearlhash`. |
| wallet | `-w <wallet>` | Merge form `PRL+NOCK`; BzMiner sends `wallet.worker`. |
| pool / failover | `-p <url>` repeated | First is primary; later URLs are rotated after disconnects. |
| worker | `--worker <name>` | Login suffix. |
| NVIDIA only | `--nvidia` | Allowlist. |
| CPU off | `--cpu 0` | Rig-wide. |
| HTTP API | `--http_enabled true`, `--http_address 127.0.0.1`, `--http_port <n>` | Unauthenticated; loopback only. |
| log | `--log-level <level>` | `network`, `debug`, `info`, `warn`, `error`; worker exposes the middle four. |
| console | `-o log`, `--no-color` | Plain container logs. |
| discovery | `--disable_udp` | Stops LAN announcements. |
| updates | `--auto-update off`, `--plugin-update off` | The image stays pinned to the shipped v100.50 binary; no runtime downloads. |
| stall guard | `--set stall_guard.enabled=false` | Prevents BzMiner restarting mining merely for a long share gap; the supervisor only restarts on process exit. |
| GPU detection | `--gpu-info` | Used only at startup to identify CUDA devices; respects visibility via CUDA. |

No device index is ever hardcoded. The worker does not set `CUDA_VISIBLE_DEVICES`.

## BzMiner statistics API

The worker polls `http://127.0.0.1:<MINER_HTTP_PORT>/api/snapshot` every 5 seconds (2-second timeout). Fields used:

- `summary.total_hashrate`, `accepted`, `rejected`
- `devices[].index`, `name`, `hashrate`, `accepted`, `rejected`, `extra["memory.dedicated.total"]` (MiB)
- `pools[].url`, `connected`, `accepted`, `rejected`

`/status` is also available from BzMiner but is not polled. A malformed or unavailable snapshot only makes readiness stale; it never kills the miner.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Clean shutdown (SIGTERM/SIGINT) or version/help success. |
| 1 | Invalid configuration, missing/inexecutable miner binary, or no NVIDIA GPU detected. |
| 2 | Unknown worker subcommand. |

BzMiner child exits are recoverable: the supervisor restarts with base 2 s, doubling to 300 s, ±20% jitter. A child that ran 5 minutes resets the attempt count. `no shares` is never a restart trigger.
