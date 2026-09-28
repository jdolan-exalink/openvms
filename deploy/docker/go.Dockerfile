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
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=${VERSION} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Commit=${COMMIT} \
      -X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/app ./apps/${APP} \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vmsctl ./apps/vmsctl

FROM debian:bookworm-slim AS runtime-ffmpeg
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --no-create-home --uid 10001 --shell /usr/sbin/nologin appuser
COPY --from=build /out/app /app
COPY --from=build /out/vmsctl /vmsctl
USER appuser
ENTRYPOINT ["/app"]

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/app /app
COPY --from=build /out/vmsctl /vmsctl
USER nonroot:nonroot
ENTRYPOINT ["/app"]
