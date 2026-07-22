FROM node:22-bookworm-slim AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-bookworm AS build
ARG GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=1 go test ./... && CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/todo ./cmd/todo

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 todo && useradd --uid 10001 --gid 10001 --no-create-home todo \
    && mkdir -p /data /backups && chown -R todo:todo /data /backups
COPY --from=build /out/todo /usr/local/bin/todo
USER todo
WORKDIR /data
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/todo", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/todo"]
