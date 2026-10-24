# Security

## Assets and non-assets

- The only configured address is a **public payout wallet**. The project never requires a private key, seed phrase, wallet file, or signing credential.
- No secrets are baked into the image or repository. `MINING_WALLET` and `NOCK_WALLET` are public addresses. Do not put API keys or registry credentials in the image; use the platform's secret/environment mechanism.
- BzMiner's log contains the pool login (`wallet.worker`) and pool URL. Treat logs as public operational data.

## Container hardening

- Runs as non-root UID/GID `10001:10001` with `HOME=/tmp` and CWD `/tmp`.
- No `--privileged`, no added capabilities, no Docker socket, no host mounts, no host network.
- No runtime package installation; `ca-certificates` is installed at build time only.
- The image does not install or modify NVIDIA kernel drivers. The NVIDIA Container Toolkit injects host userspace libraries.
- The only published port is the read-only health/metrics server (`8080`). BzMiner's API on `127.0.0.1:4014` has unauthenticated control endpoints (`POST /api/mining`, `/api/oc`) and is deliberately loopback-only. Never publish `4014` or bind it to `0.0.0.0`.
- Health endpoints are unauthenticated GET-only and expose worker name, GPU model/VRAM, pool connectivity, share counts, and hashrate. They never expose the wallet or any secret. Put the Container Gateway behind authentication if your organization requires it.
- The supervisor runs as PID 1, forwards SIGTERM/SIGINT to a process group, and force-kills after `SHUTDOWN_GRACE_SECONDS`.

## Supply chain and reproducibility

- Base images are pinned by digest (`ubuntu:24.04`, `golang:1.24-bookworm`).
- The BzMiner v100.50 Linux archive is downloaded from the official GitHub release and verified by SHA-256 (`eae8465b…960f61`) via Docker `ADD --checksum`. The extracted binary matches the release archive byte-for-byte.
- The Go worker is stdlib-only; no third-party Go modules are downloaded. Builds use `-trimpath -buildvcs=false` and a fixed Go toolchain.
- CI builds without pushing. Publishing is limited to version tags or an explicit manual run, using `GITHUB_TOKEN` with `contents: read`, `packages: write`, and `id-token: write` for attestations. No Docker Hub credentials are stored.
- Verify an image before deploying if your policy requires it: check labels, the BzMiner SHA-256, and the image digest.

## Operational risks

- **Interruptible nodes:** SaladCloud can reclaim a node at any time. The worker exits cleanly; no local state is relied on. Over-provision replicas if uptime matters.
- **Public pool/traffic:** Stratum traffic is not encrypted with `stratum+tcp`. Use `stratum+ssl` where the pool supports it; `ca-certificates` is present. The worker does not verify pool TLS by default; BzMiner's `--ssl-verify` is available through `MINER_EXTRA_ARGS` if the pool certificate is valid.
- **Unauthenticated local API:** Any process in the container could call BzMiner's loopback API. The container has no shared users or shell by default; do not add sidecars that bridge that port.
- **Wallet address mistakes:** A wrong but valid-format address sends rewards to someone else. Double-check it before scaling. The local tests do not prove payouts.
- **Terms of service:** Confirm current SaladCloud customer terms for cryptocurrency mining. SaladCloud documents mining recipes, but Chef mining and customer container mining are distinct. Do not deploy paid replicas until your organization has approval.
- **Third-party miner:** BzMiner v100.50 declares a 2% Pearl dev fee in its log. Review the BzMiner license and release notes before upgrading.
