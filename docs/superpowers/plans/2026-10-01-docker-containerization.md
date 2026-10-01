# Docker Containerization & Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a multi-stage Dockerfile and Docker Compose production configuration for POS Cafe, deploy to VPS behind Caddy SSL with internal port binding (127.0.0.1:8088), and establish an automated midnight seed cron job.

**Architecture:** A 3-stage Dockerfile (Bun builds React SPA -> Go 1.27 compiles single binary embedding SPA -> Alpine runtime). Docker Compose coordinates PostgreSQL 17 and POS Cafe on an isolated bridge network, binding strictly to 127.0.0.1:8088. Caddy handles HTTPS for pos.huuhoang.tech, and a Dockerized seed runner executes via crontab at 00:00 every night.

**Tech Stack:** Docker, Docker Compose, Go 1.27, Bun, React 19, PostgreSQL 17, Alpine Linux, Caddy.

**Spec:** `docs/superpowers/specs/2026-10-01-docker-containerization-design.md`

## Global Constraints

- Never expose PostgreSQL (5432) or internal app port (8088) to public host interfaces (0.0.0.0). Bind strictly to `127.0.0.1`.
- Container runtime must run as non-root user `nonroot` (UID 10001).
- In the repository, keep git commits strictly focused on containerization (no hardcoded VPS IPs or server credentials in repo).

---

### Task 1: Create `.dockerignore` and Multi-Stage `Dockerfile`

**Files:**
- Create: `.dockerignore`
- Create: `Dockerfile`

**Interfaces:**
- Consumes: `web/` (React SPA source), `cmd/api`, `internal/`, `api/openapi`
- Produces: Production Docker image `pos-cafe:latest` exposing port 8080 internally

- [ ] **Step 1: Create `.dockerignore`**

```dockerignore
.git
.github
.agents
.claude
.codegraph
.codex
.gemini
.pi
.playwright-mcp
.superpowers
.worktrees
%APPDATA%
bin/
build/
*.exe
*.out
*.db*
web/node_modules/
web/dist/
data/media/catalog/
resources/seeds/
docs/
```

- [ ] **Step 2: Create multi-stage `Dockerfile`**

```dockerfile
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
COPY web/embed.go ./web/
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
```

- [ ] **Step 3: Verify local file creation and git status**

Run: `git status --porcelain`
Expected: `.dockerignore` and `Dockerfile` listed as untracked.

- [ ] **Step 4: Commit Task 1**

```bash
git add .dockerignore Dockerfile
git commit -m "feat(docker): add multi-stage Dockerfile and .dockerignore"
```

---

### Task 2: Create `docker-compose.prod.yml`, `.env.prod.example`, and Makefile Targets

**Files:**
- Create: `docker-compose.prod.yml`
- Create: `.env.prod.example`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `Dockerfile`, `scripts/dev-seed.ts`, `resources/seeds/`
- Produces: Docker Compose orchestration and CLI targets (`docker-prod-build`, `docker-prod-up`, `docker-prod-down`, `docker-seed`)

- [ ] **Step 1: Create `.env.prod.example`**

```env
POSTGRES_USER=cafe_pos
POSTGRES_PASSWORD=change_this_to_a_secure_password
POSTGRES_DB=cafe_pos
APP_PORT=8088
APP_HOST_BIND=127.0.0.1
APP_ENV=production
CORS_ALLOWED_ORIGINS=*
```

- [ ] **Step 2: Create `docker-compose.prod.yml`**

```yaml
name: pos-cafe-prod

services:
  postgres:
    image: postgres:17-alpine
    container_name: pos-cafe-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-cafe_pos}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}
      POSTGRES_DB: ${POSTGRES_DB:-cafe_pos}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - pos-network
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB"]
      interval: 5s
      timeout: 5s
      retries: 5

  app:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: pos-cafe-app
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      PORT: 8080
      APP_ENV: ${APP_ENV:-production}
      DATABASE_URL: postgres://${POSTGRES_USER:-cafe_pos}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB:-cafe_pos}?sslmode=disable
      MEDIA_DIR: /app/data/media
      CORS_ALLOWED_ORIGINS: ${CORS_ALLOWED_ORIGINS:-*}
    ports:
      - "${APP_HOST_BIND:-127.0.0.1}:${APP_PORT:-8088}:8080"
    volumes:
      - media_data:/app/data/media
    networks:
      - pos-network

  seed:
    image: oven/bun:1-alpine
    profiles: ["tools"]
    working_dir: /app
    volumes:
      - .:/app:ro
      - media_data:/app/data/media
    environment:
      POS_API_BASE: http://app:8080/api/v1
      DATABASE_URL: postgres://${POSTGRES_USER:-cafe_pos}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB:-cafe_pos}?sslmode=disable
      MEDIA_DIR: /app/data/media
    networks:
      - pos-network
    command: ["bun", "run", "scripts/dev-seed.ts"]

volumes:
  postgres_data:
  media_data:

networks:
  pos-network:
    name: pos-cafe-network
```

- [ ] **Step 3: Add production compose targets to `Makefile`**

Append to `Makefile`:
```makefile
## --- Production Docker orchestration ---

docker-prod-build: ## Build the production Docker image
	docker compose -f docker-compose.prod.yml build

docker-prod-up: ## Start production containers in the background
	docker compose -f docker-compose.prod.yml up -d

docker-prod-down: ## Stop production containers
	docker compose -f docker-compose.prod.yml down

docker-prod-seed: ## Execute containerized database reset and seed
	docker compose -f docker-compose.prod.yml --profile tools run --rm seed
```

- [ ] **Step 4: Validate compose file syntax**

Run: `docker compose -f docker-compose.prod.yml config`
Expected: Valid merged compose yaml without syntax errors.

- [ ] **Step 5: Commit Task 2**

```bash
git add docker-compose.prod.yml .env.prod.example Makefile
git commit -m "feat(docker): add production docker-compose, env template, and Makefile targets"
```

---

### Task 3: Deploy to VPS, Configure Caddy SSL, and Start Containers

**Files:**
- Remote: `/home/laffy/pos-cafe/` (VPS code directory)
- Remote: `/home/laffy/pos-cafe/.env` (VPS production environment variables)
- Remote: `/etc/caddy/Caddyfile` (Caddy reverse proxy configuration)

**Interfaces:**
- Consumes: `dev` SSH profile via `ssh-mcp`, `pos.huuhoang.tech` DNS
- Produces: Live HTTPS service at `https://pos.huuhoang.tech`

- [ ] **Step 1: Synchronize project repository to VPS `/home/laffy/pos-cafe`**

Run SSH command to create directory and sync code:
```bash
mkdir -p /home/laffy/pos-cafe
```
Use git pull or archive sync via SSH MCP to populate `/home/laffy/pos-cafe`.

- [ ] **Step 2: Generate `/home/laffy/pos-cafe/.env` on VPS**

Write `.env` on VPS:
```env
POSTGRES_USER=cafe_pos
POSTGRES_PASSWORD=cafe_pos_prod_demo_98231
POSTGRES_DB=cafe_pos
APP_PORT=8088
APP_HOST_BIND=127.0.0.1
APP_ENV=production
CORS_ALLOWED_ORIGINS=*
```

- [ ] **Step 3: Update Caddyfile on VPS to route `pos.huuhoang.tech`**

Append to `/etc/caddy/Caddyfile`:
```caddyfile
pos.huuhoang.tech {
	reverse_proxy 127.0.0.1:8088 {
		header_up Host {host}
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-Proto https
	}
}
```
Reload Caddy:
```bash
sudo systemctl reload caddy
```

- [ ] **Step 4: Build and launch Docker containers on VPS**

Run on VPS:
```bash
cd /home/laffy/pos-cafe && docker compose -f docker-compose.prod.yml up -d --build
```

- [ ] **Step 5: Verify container health and HTTP responses**

Run on VPS:
```bash
docker ps --filter "name=pos-cafe"
curl -s http://127.0.0.1:8088/health
curl -s -I https://pos.huuhoang.tech/health
```
Expected: `{"status":"healthy","database":"up",...}` and HTTP 200 OK.

---

### Task 4: Run Initial Seed & Configure Midnight Reset Crontab

**Files:**
- Remote: `/home/laffy/pos-cafe/scripts/dev-seed.ts`
- Remote: `/var/spool/cron/crontabs/laffy` (User crontab on VPS)

**Interfaces:**
- Consumes: Running `pos-cafe-postgres` and `pos-cafe-app` on VPS
- Produces: Populated database with 76 menu items, tables, staff accounts, and daily 00:00 recurring reset cron

- [ ] **Step 1: Execute initial seed on VPS**

Run on VPS:
```bash
cd /home/laffy/pos-cafe && docker compose -f docker-compose.prod.yml --profile tools run --rm seed
```
Expected: `Database truncated`, `Bootstrapped admin`, `Seeded tables`, `Seeded items with images`, and exit 0.

- [ ] **Step 2: Verify seeded data via API**

Run on VPS:
```bash
curl -s http://127.0.0.1:8088/api/v1/catalog/categories | grep -o '"name":' | wc -l
```
Expected: Non-zero count of categories.

- [ ] **Step 3: Install crontab on VPS for midnight daily execution**

Add cron entry for user `laffy`:
```bash
(crontab -l 2>/dev/null | grep -v "docker-compose.prod.yml.*seed"; echo "0 0 * * * cd /home/laffy/pos-cafe && docker compose -f docker-compose.prod.yml --profile tools run --rm seed >> /home/laffy/pos-cafe/seed-cron.log 2>&1") | crontab -
```

- [ ] **Step 4: Verify crontab configuration**

Run on VPS:
```bash
crontab -l | grep "seed"
```
Expected: `0 0 * * * cd /home/laffy/pos-cafe && docker compose -f docker-compose.prod.yml --profile tools run --rm seed >> /home/laffy/pos-cafe/seed-cron.log 2>&1`
