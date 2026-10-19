# Technical Design

## Deployment topology

```
GitHub fork (anhtuan159801/goclaw, branch dev)
        │  Dockerfile build (Koyeb builder)
        ▼
Koyeb Web Service — instance type "Free" (0.1 vCPU, 512 MB, 2 GB SSD)
region: Washington, D.C. (closest to Neon aws-us-east-2)
exposed port: 18790  •  health check: HTTP GET /health, grace 300 s
        │  PORT injected by Koyeb; GOCLAW_PORT unset
        ▼
GoClaw gateway (embedded web UI)  ──runtime queries──▶  Neon pooled DATABASE_URL
docker-entrypoint.sh: goclaw upgrade ──migrations────▶  Neon direct DATABASE_URL_UNPOOLED
        │
        ▼
LLM traffic: Agnes AI  https://apihub.agnes-ai.com/v1  (provider type openai_compat)
```

## Existing behavior (verified in code)

- The gateway resolves its listening port from `GOCLAW_PORT`, then `PORT`, then the configured/default port. Dockerfile sets `EXPOSE 18790`; `docker-entrypoint.sh` runs `goclaw upgrade` before `serve`.
- Runtime stores use the application DSN (`GOCLAW_POSTGRES_DSN`, else `DATABASE_URL`). Migrations, startup schema checks, and backup/restore use the direct DSN (`GOCLAW_POSTGRES_DSN_DIRECT`, else `DATABASE_URL_UNPOOLED`) and reject a pooled URL without a direct partner (`internal/config/config.go:210`).
- `GOCLAW_HOST=0.0.0.0` is set in the Dockerfile, so `GOCLAW_GATEWAY_TOKEN` is mandatory (`internal/config/config_load.go:38`).
- `/health` is mounted before auth on both HTTP listener paths (`internal/gateway/server.go:202`, `:1095`) and is safe for platform probes.
- Web UI provider type `openai_compat` supports custom base URL + API key and is stored encrypted in PostgreSQL using `GOCLAW_ENCRYPTION_KEY`.

## Configuration contract

| Variable | Scope | Value / notes |
|---|---|---|
| `DATABASE_URL` | Koyeb secret | Neon pooled (`-pooler` host) runtime DSN |
| `DATABASE_URL_UNPOOLED` | Koyeb secret | Neon direct DSN; required for migrations and backups |
| `GOCLAW_GATEWAY_TOKEN` | Koyeb secret | Required public-bind auth token |
| `GOCLAW_ENCRYPTION_KEY` | Koyeb secret | 32-byte key (64 hex chars preferred) |
| `GOMEMLIMIT` | Koyeb env | `400MiB` — keeps Go heap under the 512 MB instance cap |
| `GOMAXPROCS` | Koyeb env | `1` — 0.1 vCPU quota; avoids over-parallel GC/schedulers |
| `GOCLAW_PG_MAX_OPEN_CONNS` | Koyeb env | `5` — fewer pooled Neon connections for a tiny instance |
| `GOCLAW_PG_MAX_IDLE_CONNS` | Koyeb env | `2` |
| `GOCLAW_PROVIDER` / `GOCLAW_MODEL` | Koyeb env, optional | Default provider/model once Agnes is registered |
| `GOCLAW_PORT` | leave unset | Koyeb `PORT` must win |

Koyeb service settings: Web Service, GitHub `anhtuan159801/goclaw`, branch `dev`, builder Dockerfile (root), instance type Free, region Washington, D.C., single instance, exposed port `18790` (HTTP), health check `GET /health` with grace period 300 s (allowed range 5–900 s), default restart limit, one replica.

## Neon database flow

1. A new claimable project is created in `aws-us-east-2` (`npx neon@latest claim create --file .env.local`), keeping DSNs only in the ignored `.env.local`.
2. `goclaw migrate up` runs locally against the direct DSN to reach schema v99 and verify with `goclaw doctor`.
3. The deployment starts with those credentials; `docker-entrypoint.sh` re-checks/migrates idempotently on boot.
4. `neon claim accept --no-open` mints a 15-minute claim URL. The user opens it and signs in to Neon. Claiming revokes the old database password and project tokens; after the transfer the user copies the new pooled/direct DSNs from the Neon console and updates `.env.local` and the two Koyeb database secrets.
5. Fallback if the user prefers their own Neon account: they create the project in the Neon console, paste both DSNs into `.env.local`, and we skip the claim ceremony.

Note: the final deployment used this fallback path — a user-owned Neon project (`golaw`, `aws-us-east-2`, branch `production`) replaced the claimable-project flow. See `implement.md` item 9.

The expired project `late-snow-76210001` is discarded; no data import is attempted.

## Agnes AI provider

- Dashboard → Providers → Add: type `openai_compat`, API base `https://apihub.agnes-ai.com/v1`, model `agnes-2.5-flash`, API key entered in the dashboard (stored encrypted via `GOCLAW_ENCRYPTION_KEY`).
- Optionally set it as the default provider/model so new agents use it without per-agent configuration.
- Vendor docs confirm streaming and tool calling. No embeddings endpoint is documented; semantic memory/knowledge graph embeddings remain unconfigured (documented as degraded) unless a separate embeddings provider is added.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Koyeb Free scales to zero after 1 h without traffic | Cold start on next request; WebSocket clients reconnect; cron/heartbeat pauses while asleep | Documented; health check grace 300 s absorbs cold start; user accepts free-tier behavior |
| Ephemeral disk, no Volumes | Uploads, workspace files, installed skills lost on redeploy/restart | Documented; database-backed data persists in Neon |
| 0.1 vCPU migration at boot | Slow first boot | Grace 300 s; migrations already applied locally to minimize boot work |
| Unclaimed Neon project expires in 72 h | Database deleted | Claim into the user's Neon account immediately after provisioning |
| Claiming revokes database password | App loses DB access until secrets updated | Claim first, then update `.env.local` and Koyeb secrets, then deploy (recommended); if claiming after deploy, redeploy with new secrets |
| Secrets printed on screen | Credential disclosure | Never render `.env.local` contents; tokens/keys generated into untracked files only |
| Windows-only vet/test failures | Local gate noise | Use `go build` locally and Linux CI/build as the gate; do not claim Windows tests green |
| Provider API key leakage | Account abuse | Key entered in dashboard only; Koyeb secret if env-based; never tracked |

## Surface parity

- Gateway/runtime: no code change expected; existing port/DSN/health contracts are reused.
- API contract: N/A — no request/response schema changes.
- Web UI: N/A — no UI code change; Agnes provider is data entered at runtime.
- CLI/runtime package + docs: update `docs/deployment-guide.md` (Koyeb Free section), `.env.example` (low-memory block), and `Dockerfile` HEALTHCHECK start period; record Koyeb Free constraints in `.trellis/spec/backend/database-guidelines.md`.
