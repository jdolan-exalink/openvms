# Builds the SPA and serves it with Caddy, which also reverse-proxies the API.
FROM node:22-alpine AS build
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/
RUN pnpm install --frozen-lockfile
COPY packages ./packages
COPY apps/web ./apps/web
RUN pnpm --filter @openvms/web build

FROM caddy:2-alpine
COPY deploy/docker/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/apps/web/dist /srv
