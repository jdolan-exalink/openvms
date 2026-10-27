# Builds the SPA and serves it with Caddy, which also reverse-proxies the API.
FROM node:22-alpine AS build
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/
RUN pnpm install --frozen-lockfile
COPY packages ./packages
COPY apps/web ./apps/web
RUN NODE_OPTIONS=--max-old-space-size=1024 pnpm --filter @openvms/web build

# Caddy is rebuilt here because the official image is built with an older Go patch release, and the image
# scan fails the build on HIGH findings that have a fix (standard library and golang.org/x/net). Same
# module set as the official image (no plugins); x/net is raised to the fixed release.
FROM golang:1.26-alpine AS caddy
ARG CADDY_VERSION=v2.11.7
# main.go is what xcaddy generates for a build without plugins.
RUN mkdir /src && cd /src && go mod init caddybuild \
 && printf 'package main\n\nimport (\n\tcaddycmd "github.com/caddyserver/caddy/v2/cmd"\n\t_ "github.com/caddyserver/caddy/v2/modules/standard"\n)\n\nfunc main() { caddycmd.Main() }\n' > main.go \
 && go get github.com/caddyserver/caddy/v2@${CADDY_VERSION} golang.org/x/net@v0.60.0 \
 && go mod tidy \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/caddy .

FROM caddy:2-alpine
# The base image lags the Alpine repositories: upgrade first so a fixed vulnerability in a base library
# (the image scan fails the build on HIGH or CRITICAL findings that have a fix) is not shipped.
# APK_REFRESH changes on every deploy (the build time) and every CI day, so the cached layer below is
# rebuilt and a library fix published since the last build is not skipped.
ARG APK_REFRESH=
RUN echo "apk refresh: ${APK_REFRESH:-none}" \
 && apk upgrade --no-cache && apk add --no-cache coreutils openssl
COPY --from=caddy /out/caddy /usr/bin/caddy
COPY deploy/docker/Caddyfile /etc/caddy/Caddyfile
COPY deploy/docker/prepare-web-tls.sh /usr/local/bin/prepare-web-tls.sh
COPY deploy/docker/web-entrypoint.sh /usr/local/bin/web-entrypoint.sh
RUN chmod 0755 /usr/local/bin/prepare-web-tls.sh /usr/local/bin/web-entrypoint.sh
COPY --from=build /src/apps/web/dist /srv
# Build metadata served as /version.json (revalidated on every load by the Caddyfile).
# VERSION is empty or "dev" when built without the deploy scripts: read the repo VERSION file then.
COPY VERSION /tmp/VERSION
ARG VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN if [ -z "$VERSION" ] || [ "$VERSION" = "dev" ]; then VERSION=$(tr -d '[:space:]' < /tmp/VERSION); fi; printf '{"version":"%s","commit":"%s","buildTime":"%s"}\n' "$VERSION" "$COMMIT" "$BUILD_TIME" > /srv/version.json
ENTRYPOINT ["/usr/local/bin/web-entrypoint.sh"]
