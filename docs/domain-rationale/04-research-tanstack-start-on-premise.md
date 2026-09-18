# Research TanStack Start on-premise operation

Type: research
Status: resolved

## Question

Using current official TanStack, Nitro, and relevant runtime documentation or source code, establish the supported production path for running this TanStack Start application on one cafe computer and serving browser clients over LAN. Identify build/runtime requirements, network binding, process supervision implications, production update constraints, and any facts that later deployment, backup, and recovery decisions must account for. Do not design or implement the deployment.

## Answer

TanStack Start can run on the cafe computer through the officially documented Vite + Nitro `node_server` path: build a standalone `.output` artifact and run `.output/server/index.mjs` under a compatible Node LTS runtime. Nitro supports LAN binding through `NITRO_HOST`/`HOST` and `NITRO_PORT`/`PORT`, but framework output alone does not supply boot-time startup, crash supervision, stable LAN addressing, firewall/TLS policy, controlled updates, PostgreSQL backup, or restore verification. Those must be resolved as appliance operations, with application artifacts, runtime configuration, and database recovery treated as separate concerns.

Full cited findings: [TanStack Start on-premise operation](../research/04-tanstack-start-on-premise.md).
