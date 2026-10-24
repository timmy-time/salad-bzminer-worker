#!/usr/bin/env bash
# Build the image and verify the no-GPU, fake-miner, and (when available)
# real-GPU paths. Never weakens checks to make them pass.
set -euo pipefail

cd "$(dirname "$0")/../.."
ROOT="$PWD"
IMAGE="${IMAGE:-salad-bzminer-worker:test}"
TMP="$ROOT/tests/integration/.tmp"
rm -rf "$TMP"
mkdir -p "$TMP"

cleanup() {
  docker rm -f bzminer-fake-test bzminer-crash-test bzminer-gpu-test >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== build image =="
docker build -t "$IMAGE" .

echo "== image version, user, labels =="
docker run --rm "$IMAGE" --version | tee "$TMP/version.txt"
grep -q "salad-bzminer-worker" "$TMP/version.txt"
grep -q "v100.50" "$TMP/version.txt"
USER_LINE=$(docker image inspect "$IMAGE" --format '{{.Config.User}}')
test "$USER_LINE" = "10001:10001"
docker image inspect "$IMAGE" --format '{{json .Config.Labels}}' | grep -q 'org.opencontainers.image.title'

echo "== no GPU exits 1 =="
set +e
docker run --rm -e NVIDIA_VISIBLE_DEVICES=void "$IMAGE" >"$TMP/nogpu.log" 2>&1
rc=$?
set -e
test "$rc" -eq 1
grep -q "no NVIDIA GPU detected" "$TMP/nogpu.log"
grep -q 'NVIDIA_VISIBLE_DEVICES=' "$TMP/nogpu.log"
grep -q 'void' "$TMP/nogpu.log"

echo "== invalid config exits 1 =="
set +e
docker run --rm -e MINING_WALLET=bad "$IMAGE" >"$TMP/badconfig.log" 2>&1
rc=$?
set -e
test "$rc" -eq 1
grep -q "MINING_WALLET" "$TMP/badconfig.log"

echo "== build fake miner =="
CGO_ENABLED=0 go build -o "$TMP/fakebzminer" ./tests/fakebzminer

echo "== fake GPU/miner: readiness, metrics, graceful stop =="
docker run -d --name bzminer-fake-test \
  -p 127.0.0.1:18080:8080 \
  -v "$TMP/fakebzminer:/tmp/fake:ro" \
  -e BZMINER_BIN=/tmp/fake \
  -e MINING_POOL=stratum+tcp://127.0.0.1:1 \
  -e HEALTH_PORT=8080 \
  "$IMAGE" >/dev/null

ready=0
for _ in $(seq 1 40); do
  if curl -fsS http://127.0.0.1:18080/readyz >"$TMP/ready.json" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
test "$ready" -eq 1
python3 - "$TMP/ready.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
assert d["status"]=="ready", d
assert d["worker"].startswith("salad-rtx4090-"), d
PY

curl -fsS http://127.0.0.1:18080/healthz | grep -q '"ok"'
curl -fsS http://127.0.0.1:18080/metrics >"$TMP/metrics.txt"
grep -q 'worker="salad-rtx4090-' "$TMP/metrics.txt"
grep -q 'bzminer_shares_accepted_total 7' "$TMP/metrics.txt"
grep -q 'bzminer_hashrate_total_hs 1234.500000' "$TMP/metrics.txt"
grep -q 'bzminer_pool_connected 1' "$TMP/metrics.txt"
! grep -q 'prl1' "$TMP/metrics.txt"

docker exec bzminer-fake-test /app/worker healthcheck
docker exec bzminer-fake-test /app/worker print-command >"$TMP/printcmd.json"
python3 - "$TMP/printcmd.json" <<'PY2'
import json,sys
d=json.load(open(sys.argv[1]))
assert d["worker_name"].startswith("salad-rtx4090-"), d
assert "--cpu" in d["argv"] and d["argv"][d["argv"].index("--cpu")+1]=="0", d
PY2

docker stop -t 20 bzminer-fake-test >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' bzminer-fake-test)" = "0"
docker logs bzminer-fake-test >"$TMP/fake.log" 2>&1
grep -q "miner started" "$TMP/fake.log"
grep -q "shutdown requested" "$TMP/fake.log"
docker rm bzminer-fake-test >/dev/null

echo "== crashing miner restarts with backoff =="
docker run -d --name bzminer-crash-test \
  -v "$TMP/fakebzminer:/tmp/fake:ro" \
  -e BZMINER_BIN=/tmp/fake \
  -e MINING_POOL=stratum+tcp://127.0.0.1:1 \
  -e FAKE_CRASH_ALWAYS=1 \
  -e RESTART_BACKOFF_BASE_SECONDS=1 \
  -e RESTART_BACKOFF_MAX_SECONDS=4 \
  "$IMAGE" >/dev/null
sleep 9
docker logs bzminer-crash-test >"$TMP/crash.log" 2>&1
test "$(grep -c 'miner exited' "$TMP/crash.log")" -ge 2
grep -q "restarting miner" "$TMP/crash.log"
docker stop -t 20 bzminer-crash-test >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' bzminer-crash-test)" = "0"
docker rm bzminer-crash-test >/dev/null

echo "== real GPU smoke (if available) =="
if docker run --rm --gpus all --entrypoint /opt/bzminer/bzminer "$IMAGE" --gpu-info >"$TMP/gpu-info.txt" 2>&1; then
  grep -q "NVIDIA CUDA devices:" "$TMP/gpu-info.txt"
  if grep -q "NVIDIA CUDA devices: 0" "$TMP/gpu-info.txt"; then
    echo "SKIP: runtime exposes no NVIDIA GPUs"
  else
    docker run -d --name bzminer-gpu-test \
      --gpus all -p 127.0.0.1:18081:8080 "$IMAGE" >/dev/null
    ready=0
    for _ in $(seq 1 120); do
      if curl -fsS http://127.0.0.1:18081/readyz >"$TMP/gpu-ready.json" 2>/dev/null; then
        ready=1
        break
      fi
      sleep 1
    done
    test "$ready" -eq 1
    curl -fsS http://127.0.0.1:18081/metrics >"$TMP/gpu-metrics.txt"
    grep -q 'bzminer_gpu_detected 1' "$TMP/gpu-metrics.txt"
    grep -q 'bzminer_gpu_memory_total_bytes' "$TMP/gpu-metrics.txt"
    docker logs bzminer-gpu-test >"$TMP/gpu.log" 2>&1
    grep -q "miner started" "$TMP/gpu.log"
    docker stop -t 30 bzminer-gpu-test >/dev/null
    test "$(docker inspect -f '{{.State.ExitCode}}' bzminer-gpu-test)" = "0"
    echo "NOTE: real-GPU hashrate/shares observed in $TMP/gpu-metrics.txt (zero is not a failure)"
    grep -E 'bzminer_hashrate_total_hs|bzminer_shares_(accepted|rejected)_total' "$TMP/gpu-metrics.txt" || true
    docker rm bzminer-gpu-test >/dev/null
  fi
else
  echo "SKIP: no NVIDIA runtime (--gpus all failed)"
fi

echo "ALL INTEGRATION CHECKS PASSED"
