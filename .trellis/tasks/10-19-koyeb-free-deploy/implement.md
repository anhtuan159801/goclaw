# Implementation Plan

## Approved scope

- One Koyeb Free Web Service for GoClaw standard edition, Neon PostgreSQL for durable data, Agnes AI as the model provider.
- Uploads/workspace files remain ephemeral; no object storage, no volumes, no production SLA.
- Execution path chosen by user: Koyeb CLI with an API token stored in untracked `.env.koyeb.local`.

## Ordered work

1. [x] Repo prep for Koyeb Free (commit `dfcbe24a`, pushed `dev`)
   - `Dockerfile`: `HEALTHCHECK --start-period` raised 10 s → 120 s.
   - `docs/deployment-guide.md`: "Koyeb With Neon", "Koyeb Free instance", Agnes `openai_compat` sections.
   - `.env.example`: commented low-memory/Koyeb block in `dfcbe24a`; the `GOCLAW_ALLOWED_ORIGINS` row was added later in the working tree (uncommitted, pending a follow-up commit).
   - `.trellis/spec/backend/database-guidelines.md`: Free-tier constraints recorded.
2. [x] Verify: `go build ./...` and `go build -tags sqliteonly ./...` pass. Windows `go vet`/`go test` blockers are pre-existing (`internal/tools/document_parser_test.go:194`), not fixed by this task.
3. [x] Neon: backups in `%TEMP%\opencode\neon-backup-2026-10-19\`. First claimable project `misty-rain-75332954` created and migrated (v99, hooks applied) — later abandoned (claim revokes the DB password; no data worth preserving). Final DB: user-owned project `golaw` (`falling-haze-94921635`, branch `production`, `aws-us-east-2`, PG 18), migrated locally with `GOCLAW_MIGRATIONS_DIR` set; direct DSN validated before wiring.
4. [x] Secrets generated into untracked `.env.koyeb.local`; never printed.
5. [x] Commit `dfcbe24a` pushed to `origin/dev` (`28afa5b6..dfcbe24a`).
6. [x] Koyeb: app `goclaw`, service `88af6e01`, Free instance, region `was`, Dockerfile builder from `github.com/anhtuan159801/goclaw@dev`, port `18790:http`, route `/:18790`, checks `18790:http:/health` grace 300 s; secrets `goclaw-database-url`, `goclaw-database-url-unpooled`, `goclaw-gateway-token`, `goclaw-encryption-key`; tuning `GOMEMLIMIT`, `GOMAXPROCS`, `GOCLAW_PG_MAX_OPEN_CONNS`, `GOCLAW_PG_MAX_IDLE_CONNS`, `GOCLAW_ALLOWED_ORIGINS`. Status HEALTHY.
7. [x] Verified: `GET /health` → 200 `{"status":"ok","protocol":3}`; UI served (HTML 200); runtime logs show startup migrations and gateway; DB switch proven via `pg_stat_activity` (2 idle `app=goclaw` sessions, matching `GOCLAW_PG_MAX_IDLE_CONNS=2`) and `builtin_tools` = 46 rows on the new Neon DB.
8. [ ] Agnes AI: user adds `openai_compat` provider (`https://apihub.agnes-ai.com/v1`, `agnes-2.5-flash`, API key entered in dashboard) and sends a test message — pending user action (setup wizard reappears on the new DB).
9. [x] Neon durability resolved alternatively: user created their own Neon project; Koyeb secrets updated with the new DSNs (direct = pooled string minus `-pooler`), service redeployed. No claim ceremony needed; the abandoned claimable project expires 2026-10-22 11:54 UTC.
10. [ ] Finish: task journal, check phase, commit task artifacts, archive.

## Validation commands

```powershell
go build ./... ; go build -tags sqliteonly ./...
go run . doctor                 # after loading .env.local into the process env
git status --short --branch     # no secrets, only intended files
```

Koyeb verification (public): `curl https://<service>.koyeb.app/health` → `{"status":"ok","protocol":3}`.

## High-risk points

- Never print or commit `.env.local`, `.env.koyeb.local`, generated tokens, encryption key, or the Agnes API key. The Koyeb API token was pasted into chat once — rotate after rollout.
- Migrations must use `DATABASE_URL_UNPOOLED`; a pooled-only config is rejected by design.
- Koyeb Free scales to zero after 1 h idle and has no persistent volume; state that plainly in docs and to the user.
- Do not claim Windows `go vet`/`go test` are green; those failures pre-date this task.

## User actions required (blockers)

1. (done) Koyeb execution path chosen: API token in `.env.koyeb.local`.
2. (done) Neon: own project created; DSNs wired via local files + Koyeb secrets.
3. Agnes API key entered in the dashboard after first boot; set provider/model as default.
4. Rotate the Koyeb API token after rollout (it was pasted into chat).

## Execution log

- 2026-10-19: repo prep; commit `dfcbe24a` pushed to `origin/dev`.
- 2026-10-19 12:10–12:13 UTC: Koyeb app/secrets/service created; first deployment HEALTHY; `/health` ok.
- 2026-10-19 12:17 UTC: `GOCLAW_ALLOWED_ORIGINS` added on the service (fixes `security.cors_open` warning); redeploy HEALTHY.
- 2026-10-19 19:27 (+07): user created Neon project `golaw` (Ohio, PG 18).
- 2026-10-19 19:34 (+07): DSNs written to `.env.local` + `.env.koyeb.local`; local `migrate up` → schema v99 + hooks on the new DB; Koyeb secrets updated; redeploy `a3a91531` HEALTHY; DB switch proven by `pg_stat_activity` and seeded `builtin_tools` (46).
- 2026-10-19: working-tree docs update adds `GOCLAW_ALLOWED_ORIGINS` to `docs/deployment-guide.md` and `.env.example` (pending commit).
