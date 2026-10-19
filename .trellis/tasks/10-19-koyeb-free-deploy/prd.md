# Deploy GoClaw to Koyeb Free with Neon and Agnes AI

## Goal

Run the standard GoClaw server as one Koyeb Free Web Service (512 MB RAM, 0.1 vCPU, 2 GB SSD) backed by Neon PostgreSQL, with Agnes AI (`agnes-2.5-flash`, OpenAI-compatible) added as the model provider, reachable over the Koyeb public HTTPS URL. The developer asked for this deployment to be performed ("có làm đi cho tôi") using the Koyeb Free instance package.

## Confirmed Facts

- Local builds pass: `go build ./...` and `go build -tags sqliteonly ./...`. `go vet ./...` and `go test ./...` fail on Windows for pre-existing reasons (test helper build tags, path/symlink assumptions); Linux CI is the intended gate.
- `Dockerfile` embeds the web UI by default, exposes 18790, installs PostgreSQL 18 client tools, and runs `/app/docker-entrypoint.sh` which applies `goclaw upgrade` before `serve`. The Docker `HEALTHCHECK` currently has `--start-period=10s`.
- Koyeb Free (Koyeb instance docs, 2026): 512 MB RAM / 0.1 vCPU / 2 GB SSD; one free instance per organization; only Frankfurt or Washington, D.C.; Web Services only; no custom scaling; no Volumes; scales down to zero after 1 hour without traffic; "should not be used for production applications".
- Koyeb health checks: grace period minimum 5 s, maximum 900 s; startup checks run every second; HTTP probe must return 2xx/3xx.
- Neon project `late-snow-76210001` (us-east-2) reports state `expired` in `neon claim status` (claim window ended 2026-10-18T16:24:38.847Z) although its DSN still answers and reports schema v99. It can no longer be claimed and must not be the durable database for this deployment.
- Claimable Neon defaults to `aws-us-east-2`; unclaimed projects expire in 72 h; claim codes last 15 min; claiming revokes the project key, access tokens, and database password before transfer.
- Config contract: runtime uses `GOCLAW_POSTGRES_DSN` first, then `DATABASE_URL` (pooled); migrations/startup schema checks use `GOCLAW_POSTGRES_DSN_DIRECT` first, then `DATABASE_URL_UNPOOLED`, with fallback only for direct non-pooler DSNs. A `-pooler` URL without a direct DSN is rejected for migrations (`internal/config/config.go:210`).
- `GOCLAW_GATEWAY_TOKEN` is required when `GOCLAW_HOST` is non-loopback (`internal/config/config_load.go:38`). `GOCLAW_ENCRYPTION_KEY` must be 32 bytes (64 hex chars, 44-char base64, or raw 32 bytes; `internal/crypto/aes.go:127`) and gates encrypted provider credentials and webhooks.
- PG pool env: `GOCLAW_PG_MAX_OPEN_CONNS` (default 25), `GOCLAW_PG_MAX_IDLE_CONNS` (default 10) in `internal/store/pg/pool.go`.
- Koyeb injects `PORT`; GoClaw resolves `GOCLAW_PORT` then `PORT` then default 18790. `/health` is unauthenticated (`internal/gateway/server.go:202`) and returns JSON.
- Agnes AI: OpenAI-compatible base `https://apihub.agnes-ai.com/v1`, model `agnes-2.5-flash`; the vendor docs confirm streaming and tool calling. GoClaw supports provider type `openai_compat` through the dashboard (`ui/web/src/constants/providers.ts`). No `/embeddings` endpoint is documented for Agnes, so semantic-memory embeddings need a separate provider.
- `.env.local` (untracked) holds the current Neon pooled/direct DSNs. `GOCLAW_GATEWAY_TOKEN`, `GOCLAW_ENCRYPTION_KEY`, and any Agnes API key are absent from the environment and repo.
- `gh` is authenticated as `anhtuan159801`; `origin` = `https://github.com/anhtuan159801/goclaw`, default branch `dev`, currently 5 commits ahead of `origin/dev` (includes the Neon/Koyeb support commit `532d2c94`). No Koyeb CLI, Docker, or Neon CLI is installed; `npx neon@latest` works.

## Requirements

- Deploy as a Koyeb Web Service built from the GitHub fork `anhtuan159801/goclaw` branch `dev` using the root `Dockerfile`, region Washington, D.C. (closest to Neon `aws-us-east-2`), instance type Free, one instance.
- Exposed HTTP port 18790; HTTP health check `GET /health`; startup grace period 300 s (max 900 s); default restart limit.
- Koyeb runtime env: `DATABASE_URL`, `DATABASE_URL_UNPOOLED`, `GOCLAW_GATEWAY_TOKEN`, `GOCLAW_ENCRYPTION_KEY` as secrets; low-memory tuning `GOMEMLIMIT=400MiB`, `GOMAXPROCS=1`, `GOCLAW_PG_MAX_OPEN_CONNS=5`, `GOCLAW_PG_MAX_IDLE_CONNS=2`. Leave `GOCLAW_PORT` unset so Koyeb `PORT` wins.
- Keep startup migrations enabled through the Docker entrypoint; verify Neon reaches schema v99 with data hooks applied.
- Provide a durable Neon database (new claimable project in `aws-us-east-2`, claimed into the user's Neon account, or a project the user creates in their own account).
- Add Agnes AI in the dashboard: type `openai_compat`, base URL `https://apihub.agnes-ai.com/v1`, model `agnes-2.5-flash`; keep the API key only in the dashboard or Koyeb secrets. Optionally set it as the default provider/model.
- Update repository documentation for Koyeb Free constraints (scale-to-zero, ephemeral disk, one free instance, region restriction, 512 MB tuning) and align the Docker HEALTHCHECK start period with slow cold starts.
- No secret in any tracked file; credentials stay in untracked local files or Koyeb secrets.
- Push the prepared commits to `origin/dev` so Koyeb can build them.

## Acceptance Criteria

- [ ] `go build ./...` and `go build -tags sqliteonly ./...` pass after repo changes.
- [ ] Docs describe Koyeb Free settings (region, instance type, port, `/health`, 300 s grace, env table, scale-to-zero and ephemeral-disk caveats) and `.env.example` documents the low-memory/Koyeb env block without secrets.
- [ ] Docker HEALTHCHECK `--start-period` reflects slow cold starts (>= 120 s) and still probes the resolved `GOCLAW_PORT`/`PORT` port.
- [ ] A fresh Neon project in `aws-us-east-2` is attached to the deployment and reports schema v99 with all data hooks applied.
- [ ] The Koyeb service built from fork `dev` becomes healthy; `GET https://<service>.koyeb.app/health` returns `{"status":"ok",...}`.
- [ ] The embedded web UI loads at the service URL and can be used with the gateway token.
- [ ] Agnes AI (`openai_compat`, `agnes-2.5-flash`) is configured and a test message succeeds; if no embeddings provider is configured, semantic-memory features are documented as degraded, not claimed working.
- [ ] No secret appears in any tracked file or commit; `git status` shows only intended changes.
- [ ] Trellis quality gates pass and the task is archived with the session journal updated.

## Out of Scope

- Object storage or persistent volumes (Koyeb Free does not support Volumes; uploads/workspace files stay ephemeral).
- High availability, autoscaling, or production SLA (Koyeb Free is explicitly not for production).
- Migrating data from SQLite or the expired Neon project.
- Upstreaming changes to `nextlevelbuilder/goclaw` (work stays on the fork).
- Embedding provider provisioning for Agnes (no documented embeddings endpoint).

## Open Questions

1. Koyeb execution path: provide a Koyeb API token via local env so the deployment can run through the Koyeb CLI, or keep it manual through the Koyeb console? Recommendation: CLI with a locally stored token, then verify the public URL.
2. Neon: create a new claimable project now and claim it into the user's Neon account (15-minute claim link), or use a project the user creates in their own Neon account? Recommendation: claimable project, claimed immediately before deployment.
3. Agnes API key: entered in the dashboard after first boot (never pasted into chat)? Recommendation: yes.
