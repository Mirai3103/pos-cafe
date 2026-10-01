# Architecture Specification: Docker Containerization & Production Compose

## 1. Overview

This document specifies the containerization architecture for POS Cafe. The application consists of a single Go binary embedding a compiled React 19 SPA (`web/dist`), connected to PostgreSQL 17. Embedded database migrations run automatically on startup via PostgreSQL advisory locks.

The containerization design provides:
1. A reproducible multi-stage Docker build producing a minimal, secure production image.
2. A production-oriented Docker Compose configuration coordinating the application, PostgreSQL, persistent storage volumes, and internal networking.
3. A reproducible containerized mechanism to execute database reset and seed operations (`scripts/dev-seed.ts`).

---

## 2. Multi-Stage Dockerfile Architecture

The `Dockerfile` employs a 3-stage build process to optimize caching, build speed, and final image size:

### Stage 1: Frontend Builder (`web-builder`)
- **Base image:** `oven/bun:1-alpine`
- **Working directory:** `/build/web`
- **Actions:**
  - Copy `web/package.json` and `web/bun.lock*` (or equivalent lockfiles).
  - Run `bun install --frozen-lockfile` (with caching if available).
  - Copy remaining `web/` source code.
  - Run `bun run build` to produce production assets in `/build/web/dist`.

### Stage 2: Backend Builder (`backend-builder`)
- **Base image:** `golang:1.24-alpine`
- **Working directory:** `/build`
- **Environment:** `CGO_ENABLED=0`, `GOOS=linux`
- **Actions:**
  - Install build essentials (`git`, `ca-certificates`).
  - Copy `go.mod` and `go.sum`.
  - Download Go dependencies via `go mod download`.
  - Copy backend source files (`cmd/`, `internal/`, `api/`).
  - Copy `/build/web/dist` from `web-builder` into `web/dist` inside the build context.
  - Compile the Go binary:
    ```sh
    go build -trimpath -ldflags="-s -w" -o /build/bin/pos-cafe ./cmd/api
    ```

### Stage 3: Minimal Runtime (`runner`)
- **Base image:** `alpine:3.21`
- **Security:** Run as an unprivileged user (`nonroot:nonroot`, UID/GID 10001).
- **Installed packages:** `ca-certificates`, `tzdata` (for correct timezone calculation).
- **Files copied:**
  - Binary from `backend-builder`: `/build/bin/pos-cafe` -> `/app/pos-cafe`.
- **Directories & Volumes:**
  - `/app/data/media` with ownership `10001:10001` for uploaded catalog media.
- **Exposed Port:** `8080` (internal container port).
- **Healthcheck:**
  ```dockerfile
  HEALTHCHECK --interval=10s --timeout=3s --retries=3 --start-period=5s \
    CMD wget -qO- http://localhost:8080/health || exit 1
  ```
- **Entrypoint:** `/app/pos-cafe`

---

## 3. Docker Compose Production Configuration (`docker-compose.prod.yml`)

The production compose manifest coordinates application dependencies without exposing sensitive internal databases to external interfaces.

### Services

#### `postgres`
- **Image:** `postgres:17-alpine`
- **Container Name:** `pos-cafe-postgres`
- **Restart Policy:** `unless-stopped`
- **Environment:**
  - `POSTGRES_USER`: `${POSTGRES_USER:-cafe_pos}`
  - `POSTGRES_PASSWORD`: `${POSTGRES_PASSWORD}`
  - `POSTGRES_DB`: `${POSTGRES_DB:-cafe_pos}`
- **Volumes:**
  - `postgres_data:/var/lib/postgresql/data`
- **Networks:** `pos-network` (isolated internal bridge network, no public host port binding).
- **Healthcheck:**
  ```yaml
  test: ["CMD-SHELL", "pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB"]
  interval: 5s
  timeout: 5s
  retries: 5
  ```

#### `app`
- **Build Context:** `.` (using `Dockerfile`)
- **Container Name:** `pos-cafe-app`
- **Restart Policy:** `unless-stopped`
- **Depends On:**
  - `postgres`: condition `service_healthy`
- **Environment:**
  - `PORT=8080`
  - `APP_ENV=production`
  - `DATABASE_URL=postgres://${POSTGRES_USER:-cafe_pos}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB:-cafe_pos}?sslmode=disable`
  - `MEDIA_DIR=/app/data/media`
  - `CORS_ALLOWED_ORIGINS=*`
  - `HTTP_READ_TIMEOUT=15s`
  - `HTTP_WRITE_TIMEOUT=15s`
  - `HTTP_IDLE_TIMEOUT=60s`
  - `HTTP_READ_HEADER_TIMEOUT=5s`
- **Ports:**
  - Bound internally to localhost: `${APP_HOST_BIND:-127.0.0.1}:${APP_PORT:-8088}:8080`
- **Volumes:**
  - `media_data:/app/data/media`
- **Networks:** `pos-network`

### Volumes
- `postgres_data`: Persistent storage for PostgreSQL database cluster.
- `media_data`: Persistent storage for catalog item images and content-addressed media.

### Networks
- `pos-network`: Dedicated bridge network for internal communication between `app` and `postgres`.

---

## 4. Containerized Seed Service

For environments requiring scheduled or on-demand reset and seeding (`dev-seed.ts`), a one-off profile service `seed` is included in Compose:

```yaml
  seed:
    image: oven/bun:1-alpine
    profiles: ["tools"]
    working_dir: /app
    volumes:
      - .:/app:ro
    environment:
      - POS_API_BASE=http://app:8080/api/v1
      - DATABASE_URL=postgres://${POSTGRES_USER:-cafe_pos}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB:-cafe_pos}?sslmode=disable
      - MEDIA_DIR=/app/data/media
    networks:
      - pos-network
    command: ["bun", "run", "scripts/dev-seed.ts"]
```

This guarantees that demo resets run reproducibly in the exact same network without requiring local runtime installations on the host.

---

## 5. Security & Isolation Invariants

1. **Least Privilege Runtime:** Application container runs as non-root user `nonroot` (10001).
2. **Network Isolation:** PostgreSQL is not bound to any host interface; it is reachable solely by containers on `pos-network`.
3. **Application Port Binding:** Bound strictly to `127.0.0.1` by default via `${APP_HOST_BIND:-127.0.0.1}` to prevent unintentional public port exposure.
4. **Data Durability:** Named volumes protect both tabular data (`postgres_data`) and binary media assets (`media_data`).
