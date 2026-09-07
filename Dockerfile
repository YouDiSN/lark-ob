# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS ui-builder
WORKDIR /src/internal/server/ui
COPY internal/server/ui/package.json internal/server/ui/package-lock.json ./
RUN npm ci
COPY internal/server/ui/ ./
RUN npm run build

FROM golang:1.24-bookworm AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /src/internal/server/ui/dist/ ./internal/server/ui/dist/
ARG VERSION=0.1.0
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/lark-ob ./cmd/lark-ob

FROM node:22-bookworm-slim AS runtime
ARG LARK_CLI_VERSION=1.0.93
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tini tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && npm install --global "@larksuite/cli@${LARK_CLI_VERSION}" \
    && npm cache clean --force \
    && install -d -o node -g node /app /data /cache/lark-ob

COPY --from=go-builder /out/lark-ob /usr/local/bin/lark-ob

ENV HOME=/home/node \
    XDG_CACHE_HOME=/cache \
    LARK_CLI_PATH=/usr/local/bin/lark-cli \
    LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1 \
    LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1

USER node
WORKDIR /app
EXPOSE 8765

HEALTHCHECK --interval=20s --timeout=5s --start-period=20s --retries=3 \
    CMD curl --fail --silent --show-error http://127.0.0.1:8765/api/status >/dev/null || exit 1

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["lark-ob", "start", "--no-open", "--listen", "127.0.0.1:8765", "--data", "/data/lark-ob.db"]
