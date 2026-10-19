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

FROM caddy:2-alpine
RUN apk add --no-cache coreutils openssl
COPY deploy/docker/Caddyfile /etc/caddy/Caddyfile
COPY deploy/docker/prepare-web-tls.sh /usr/local/bin/prepare-web-tls.sh
COPY deploy/docker/web-entrypoint.sh /usr/local/bin/web-entrypoint.sh
RUN chmod 0755 /usr/local/bin/prepare-web-tls.sh /usr/local/bin/web-entrypoint.sh
COPY --from=build /src/apps/web/dist /srv
# Build metadata served as /version.json (revalidated on every load by the Caddyfile).
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN printf '{"version":"%s","commit":"%s","buildTime":"%s"}\n' "$VERSION" "$COMMIT" "$BUILD_TIME" > /srv/version.json
ENTRYPOINT ["/usr/local/bin/web-entrypoint.sh"]
