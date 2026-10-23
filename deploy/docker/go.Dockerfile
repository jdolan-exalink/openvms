# Builds one Go binary from apps/<APP> into a minimal non-root image, plus the
# vmsctl admin CLI at /vmsctl (`docker compose exec api /vmsctl bootstrap`).
#   docker build -f deploy/docker/go.Dockerfile --build-arg APP=api .
# Two runtime targets share the same build stage:
#   - runtime-ffmpeg: debian:bookworm-slim + ffmpeg, non-root. Only the worker needs this
#     (PDW-4's clip watermark job shells out to ffmpeg); selected via docker-compose.yml's
#     `target: runtime-ffmpeg` on the worker service only.
#   - runtime (default, defined LAST so a build with no --target still gets it):
#     gcr.io/distroless/static-debian12, no shell, no package manager. Every other app
#     (api, frigate-mock, vmsctl) stays on this, unaffected by the target above.
FROM golang:1.26-alpine AS build
ARG APP
# VERSION / AGENT_VERSION: when empty or "dev" they are read from VERSION and
# deploy/agent/VERSION (the single sources of truth), so a plain `docker compose up`
# still produces versioned images.
ARG VERSION=
ARG AGENT_VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY VERSION ./VERSION
COPY apps ./apps
COPY internal ./internal
COPY migrations ./migrations
COPY deploy/agent ./deploy/agent
COPY gen ./gen
COPY proto ./proto
COPY pkg ./pkg
RUN VERSION="${VERSION}"; AGENT_VERSION="${AGENT_VERSION}"; \
    if [ -z "$VERSION" ] || [ "$VERSION" = "dev" ]; then VERSION=$(tr -d '[:space:]' < VERSION); fi; \
    if [ -z "$AGENT_VERSION" ] || [ "$AGENT_VERSION" = "dev" ]; then AGENT_VERSION=$(tr -d '[:space:]' < deploy/agent/VERSION); fi; \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=${VERSION} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Commit=${COMMIT} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/app ./apps/${APP} \
 && CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=${AGENT_VERSION} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Commit=${COMMIT} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/edge-agent ./apps/edge-agent \
 && sha256sum /out/edge-agent | awk '{print $1}' > /out/edge-agent.sha256 \
 && go env GOARCH > /out/edge-agent.goarch \
 && printf '%s' "${AGENT_VERSION}" > /out/edge-agent.version \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vmsctl ./apps/vmsctl

# The worker links onnxruntime and ships the vehicle-body model. The API stays
# on the alpine build above.
FROM golang:1.26-bookworm AS build-worker
ARG VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY VERSION ./VERSION
COPY apps ./apps
COPY internal ./internal
COPY migrations ./migrations
COPY deploy/agent ./deploy/agent
COPY gen ./gen
COPY proto ./proto
COPY pkg ./pkg
COPY models/vehicle-body.onnx /opt/openvms/vehicle-body.onnx
RUN VERSION="${VERSION}"; \
    if [ -z "$VERSION" ] || [ "$VERSION" = "dev" ]; then VERSION=$(tr -d '[:space:]' < VERSION); fi; \
    curl -fsSL --retry 5 --retry-delay 2 --retry-connrefused --retry-all-errors -o /tmp/ort.tgz https://github.com/microsoft/onnxruntime/releases/download/v1.29.0/onnxruntime-linux-x64-1.29.0.tgz \
 && tar -xzf /tmp/ort.tgz -C /tmp \
 && cp /tmp/onnxruntime-linux-x64-1.29.0/lib/libonnxruntime.so.1.29.0 /opt/openvms/libonnxruntime.so.1.29.0 \
 && chmod 0644 /opt/openvms/vehicle-body.onnx /opt/openvms/libonnxruntime.so.1.29.0 \
 && rm -rf /tmp/ort.tgz /tmp/onnxruntime-linux-x64-1.29.0 \
 && CGO_ENABLED=1 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=${VERSION} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Commit=${COMMIT} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/app ./apps/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vmsctl ./apps/vmsctl

# Build the optional HailoRT user-space SDK independently so worker source
# changes do not invalidate this comparatively expensive native build.
FROM golang:1.26-bookworm AS build-hailort-sdk
ARG HAILORT_VERSION
RUN test -n "${HAILORT_VERSION}" \
 && apt-get update \
 && apt-get install -y --no-install-recommends build-essential cmake git libusb-1.0-0-dev \
 && rm -rf /var/lib/apt/lists/* \
 && git clone --depth 1 --branch "v${HAILORT_VERSION}" https://github.com/hailo-ai/hailort.git /tmp/hailort \
 && cmake -S /tmp/hailort -B /tmp/hailort/build \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX=/usr \
    -DCMAKE_INSTALL_LIBDIR=lib \
    -DHAILO_BUILD_GSTREAMER=OFF \
    -DHAILO_BUILD_EXAMPLES=OFF \
    -DHAILO_BUILD_UT=OFF \
 && cmake --build /tmp/hailort/build --parallel 2 \
 && cmake --install /tmp/hailort/build \
 && rm -rf /tmp/hailort

# Optional Hailo worker variant; the ordinary worker target stays Hailo-free.
FROM build-worker AS build-worker-hailo
ARG VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
COPY --from=build-hailort-sdk /usr/include/hailo /usr/include/hailo
COPY --from=build-hailort-sdk /usr/lib/libhailort.so* /usr/lib/
RUN VERSION="${VERSION}"; \
    if [ -z "$VERSION" ] || [ "$VERSION" = "dev" ]; then VERSION=$(tr -d '[:space:]' < VERSION); fi; \
    CGO_ENABLED=1 go build -tags hailo -trimpath \
    -ldflags "-s -w \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=${VERSION} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Commit=${COMMIT} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/app ./apps/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vmsctl ./apps/vmsctl

FROM debian:bookworm-slim AS runtime-ffmpeg
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates libstdc++6 \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --no-create-home --uid 10001 --shell /usr/sbin/nologin appuser
COPY --from=build-worker /out/app /app
COPY --from=build-worker /out/vmsctl /vmsctl
COPY --from=build-worker /opt/openvms /opt/openvms
USER appuser
ENTRYPOINT ["/app"]

FROM debian:bookworm-slim AS runtime-hailo
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates libstdc++6 libusb-1.0-0 \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --no-create-home --uid 10001 --shell /usr/sbin/nologin appuser
COPY --from=build-worker-hailo /out/app /app
COPY --from=build-worker-hailo /out/vmsctl /vmsctl
COPY --from=build-worker-hailo /opt/openvms /opt/openvms
COPY --from=build-hailort-sdk /usr/lib/libhailort.so* /usr/lib/
ENV LD_LIBRARY_PATH=/usr/lib
USER appuser
ENTRYPOINT ["/app"]

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/app /app
COPY --from=build /out/vmsctl /vmsctl
COPY --from=build /out/edge-agent /opt/openvms/edge-agent
COPY --from=build /out/edge-agent.sha256 /opt/openvms/edge-agent.sha256
COPY --from=build /out/edge-agent.goarch /opt/openvms/edge-agent.goarch
COPY --from=build /out/edge-agent.version /opt/openvms/edge-agent.version
USER nonroot:nonroot
ENTRYPOINT ["/app"]
