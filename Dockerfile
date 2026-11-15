# syntax=docker/dockerfile:1

# 1. Web UI (web/ -> web/dist). Skipped when web/ has no package.json.
FROM node:24.22.1-alpine3.24 AS web
WORKDIR /web
COPY web/ ./
RUN if [ -f package.json ]; then npm ci --no-audit --no-fund && npm run build; fi && mkdir -p dist

# 2. Server binary with the UI embedded (internal/webui/dist).
FROM golang:1.27.2-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/dist/ ./internal/webui/dist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/agent-tasks ./cmd/agent-tasks

# 3. Runtime.
FROM alpine:3.24.3
RUN addgroup -S -g 10001 app && adduser -S -u 10001 -G app -h /data app \
    && mkdir -p /data && chown app:app /data
COPY --from=build /out/agent-tasks /usr/local/bin/agent-tasks
USER app
WORKDIR /data
ENV AGENT_TASKS_LISTEN=:8080 \
    AGENT_TASKS_DB=/data/agent-tasks.db
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["agent-tasks"]
CMD ["serve"]
