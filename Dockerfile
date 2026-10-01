# Stage 1: Build React SPA
FROM oven/bun:1-alpine AS web-builder
WORKDIR /build/web

COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile

COPY web/ ./
RUN bun run build

# Stage 2: Build Go binary embedding web/dist
FROM golang:alpine AS backend-builder
WORKDIR /build
ENV CGO_ENABLED=0 GOOS=linux

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY api/ ./api/
COPY web/*.go ./web/
COPY --from=web-builder /build/web/dist ./web/dist

RUN go build -trimpath -ldflags="-s -w" -o /build/bin/pos-cafe ./cmd/api

# Stage 3: Minimal runtime
FROM alpine:3.21 AS runner

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S nonroot \
    && adduser -u 10001 -S nonroot -G nonroot

WORKDIR /app

COPY --from=backend-builder /build/bin/pos-cafe /app/pos-cafe

RUN mkdir -p /app/data/media && chown -R nonroot:nonroot /app

USER nonroot:nonroot

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=3 --start-period=5s \
  CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/pos-cafe"]
