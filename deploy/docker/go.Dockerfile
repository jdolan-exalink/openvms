# Builds one Go binary from apps/<APP> into a minimal non-root image, plus the
# vmsctl admin CLI at /vmsctl (`docker compose exec api /vmsctl bootstrap`).
#   docker build -f deploy/docker/go.Dockerfile --build-arg APP=api .
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

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build /out/vmsctl /vmsctl
USER nonroot:nonroot
ENTRYPOINT ["/app"]
