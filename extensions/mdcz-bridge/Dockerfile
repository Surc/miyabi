# syntax=docker/dockerfile:1.7
# Build separately after validation is authorized; no release workflow is added.
FROM node:24-bookworm-slim AS builder
ARG TARGETARCH=amd64
RUN test "$TARGETARCH" = amd64
ENV ELECTRON_SKIP_BINARY_DOWNLOAD=1 SKIP_INSTALL_SIMPLE_GIT_HOOKS=1
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
RUN corepack enable && corepack prepare pnpm@10.28.2 --activate
WORKDIR /repo
RUN git init && git remote add origin https://github.com/ShotHeadman/mdcz.git && git fetch --depth 1 origin f65e7d4161151aff1531f3b99872e49234d5d539 && git checkout --detach FETCH_HEAD
COPY extensions/mdcz-bridge /repo/apps/miyabi-bridge
RUN CI=true ELECTRON_SKIP_BINARY_DOWNLOAD=1 SKIP_INSTALL_SIMPLE_GIT_HOOKS=1 pnpm --filter @mdcz/server... install --frozen-lockfile
RUN pnpm --filter @mdcz/server exec vite build --config ../miyabi-bridge/vite.config.ts
RUN node apps/miyabi-bridge/stage.mjs

FROM node:24-bookworm-slim AS dependencies
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@10.28.2 --activate
COPY --from=builder /repo/bridge-staging/ ./
RUN CI=true pnpm install --prod --frozen-lockfile

FROM node:24-bookworm-slim
LABEL org.opencontainers.image.source="https://github.com/Surc/miyabi"
LABEL org.opencontainers.image.licenses="GPL-3.0-only"
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends fonts-noto-cjk ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=dependencies /app/ ./
RUN mkdir -p /data && chown node:node /data
USER node
ENV NODE_ENV=production PORT=3840
EXPOSE 3840
VOLUME /data
CMD ["node", "bridge.js"]
