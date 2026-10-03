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
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps ./apps
COPY internal ./internal
COPY migrations ./migrations
COPY deploy/agent ./deploy/agent
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=${VERSION} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Commit=${COMMIT} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/app ./apps/${APP} \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/edge-agent ./apps/edge-agent \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vmsctl ./apps/vmsctl

# The worker links onnxruntime and ships the vehicle-body model. The API stays
# on the alpine build above.
FROM golang:1.26-bookworm AS build-worker
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps ./apps
COPY internal ./internal
COPY migrations ./migrations
COPY models/vehicle-body.onnx /opt/openvms/vehicle-body.onnx
RUN curl -fsSL -o /tmp/ort.tgz https://github.com/microsoft/onnxruntime/releases/download/v1.29.0/onnxruntime-linux-x64-1.29.0.tgz \
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

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/app /app
COPY --from=build /out/vmsctl /vmsctl
COPY --from=build /out/edge-agent /opt/openvms/edge-agent
USER nonroot:nonroot
ENTRYPOINT ["/app"]
