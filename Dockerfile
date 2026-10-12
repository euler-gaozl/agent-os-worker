FROM golang:1.27.2-alpine AS build
WORKDIR /source
ENV GOWORK=off GOPROXY=off CGO_ENABLED=0
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY worker ./worker
RUN go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/service ./cmd/worker && go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck
FROM node:24-alpine AS harness
WORKDIR /app/harness
COPY harness/package.json harness/pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@11.25.0 --activate && pnpm install --frozen-lockfile --ignore-scripts --prod
COPY harness/runner.mjs ./
FROM node:24-alpine
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 agentos
WORKDIR /app
COPY --from=build /out/ /app/
COPY --from=harness /app/harness /app/harness
LABEL org.agentos.worker-engines='["legacy-go","dsh-0.2.0-rc.2","dsh-0.2.0-rc.2-evidence-v1"]'
LABEL org.agentos.builtin-tools='["knowledge_search","note_save","artifact_read","spawn_agent","sandbox_exec","document_read","run_recall","note_revise","note_retire"]'
ENV LISTEN=0.0.0.0:8099 HEALTH_URL=http://127.0.0.1:8099/readyz AGENTOS_NODE=/usr/local/bin/node DSH_RUNNER=/app/harness/runner.mjs
EXPOSE 8099
USER 10001
HEALTHCHECK --interval=15s --timeout=4s --start-period=10s CMD ["/app/healthcheck"]
ENTRYPOINT ["/app/service"]
CMD ["--config", "/run/service.json"]
