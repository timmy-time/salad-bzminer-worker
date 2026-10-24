# syntax=docker/dockerfile:1.7
#
# BzMiner v100.50 official release image (bzminer/bzminer) was not pullable
# from this environment (Docker Hub 404, GHCR 401). This image therefore uses
# the official release binary from the v100.50 release archive, verified by
# SHA-256, on the same Ubuntu 24.04 runtime base the upstream Dockerfile
# documents. No GPU userspace is installed: the NVIDIA Container Toolkit
# injects libcuda/libnvidia-ml from the host at --gpus all time.

ARG BZMINER_VERSION=v100.50
ARG BZMINER_SHA256=eae8465b0714c448d6897679dde7a893a1caa1b17bc418b2a0e875ebb2960f61
ARG BZMINER_URL=https://github.com/bzminer/bzminer/releases/download/v100.50/bzminer_v100.50_linux.tar.gz

FROM golang:1.24-bookworm@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

ARG VERSION=dev
ARG REVISION=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -buildvcs=false \
    -ldflags "-s -w -buildid= \
      -X salad-bzminer-worker/internal/version.Version=${VERSION} \
      -X salad-bzminer-worker/internal/version.Revision=${REVISION} \
      -X salad-bzminer-worker/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/worker ./cmd/worker

FROM ubuntu:24.04@sha256:68ee22004a9109b44973313b02ff7b1de250f1de0f5ecb628d73f579aaff33ac

ARG VERSION=dev
ARG REVISION=unknown
ARG BUILD_DATE=unknown
ARG BZMINER_VERSION
ARG BZMINER_SHA256
ARG BZMINER_URL
ARG IMAGE_SOURCE=https://github.com/timmy-time/salad-bzminer-worker

LABEL org.opencontainers.image.title="Salad BzMiner Worker" \
      org.opencontainers.image.description="BzMiner PEARL/NOCK GPU worker for SaladCloud Container Groups" \
      org.opencontainers.image.source="${IMAGE_SOURCE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.base.name="ubuntu:24.04" \
      com.salad.bzminer.version="${BZMINER_VERSION}"

# ca-certificates is required for stratum+ssl pools. The BzMiner binary is
# statically linked enough to start on a bare Ubuntu 24.04 (verified).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ADD --checksum=sha256:${BZMINER_SHA256} ${BZMINER_URL} /tmp/bzminer.tar.gz
RUN mkdir -p /opt/bzminer \
    && tar -xzf /tmp/bzminer.tar.gz -C /opt/bzminer --strip-components=1 \
         bzminer_v100.50_linux/bzminer \
    && chmod 0755 /opt/bzminer/bzminer \
    && rm -f /tmp/bzminer.tar.gz

COPY --from=build /out/worker /app/worker
COPY config/defaults.env /app/config/defaults.env
RUN chmod 0755 /app/worker \
    && groupadd --system --gid 10001 miner \
    && useradd --system --uid 10001 --gid 10001 --home-dir /tmp --shell /usr/sbin/nologin miner

# BzMiner writes bzminer.worker.log into its CWD; /tmp is writable by the
# unprivileged user. The health port is above 1024.
ENV HOME=/tmp \
    NVIDIA_DRIVER_CAPABILITIES=compute,utility

WORKDIR /tmp
USER 10001:10001
ENTRYPOINT ["/app/worker"]
CMD ["run"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=180s --retries=3 \
    CMD ["/app/worker","healthcheck"]

