# Technical design

## Existing behavior

The default gateway store setup requires PostgreSQL at cmd/gateway_stores_pg.go:19. internal/store/pg/factory.go:11 constructs one PG pool and wires sessions, memory, context, episodic, knowledge graph, and vault stores to it. The memory interceptor routes MEMORY.md and memory/* operations to the MemoryStore at internal/tools/memory_interceptor.go:78. The context file interceptor routes agent files to AgentStore at internal/tools/context_file_interceptor.go:74; the PG implementation reads and writes those files in internal/store/pg/agents_context.go:21.

The gateway's database config is environment-only at internal/config/config.go:192, and config loading currently reads GOCLAW_POSTGRES_DSN at internal/config/config_load.go:295. Runtime store creation and schema checks currently share that DSN at cmd/gateway_stores_pg.go:24. Migration CLI resolution uses the same field at cmd/migrate.go:68; the upgrade command and startup compatibility check do too at cmd/upgrade.go:103 and cmd/upgrade.go:204.

The gateway config reads GOCLAW_PORT at internal/config/config_load.go:254. The Dockerfile defaults ENABLE_EMBEDUI to false at Dockerfile:5, sets GOCLAW_PORT=18790 at Dockerfile:179, and hardcodes 18790 in its health check at Dockerfile:188. docker-entrypoint.sh runs upgrade only when GOCLAW_POSTGRES_DSN is set at docker-entrypoint.sh:120.

Neon CLI installs a pooled DATABASE_URL and direct DATABASE_URL_UNPOOLED. Neon recommends pooled connections for application queries and direct connections for migrations and backup/restore; the pooled endpoint uses PgBouncer transaction mode. Sources: [Neon connection pooling](https://neon.com/docs/connect/connection-pooling) and the installed Neon Postgres skill. Neon enables pgvector through ordinary PostgreSQL migrations; GoClaw's initial migration creates pgcrypto and vector at migrations/000001_init_schema.up.sql:4.

## Data flow

1. The standard server reads its application DSN from GOCLAW_POSTGRES_DSN when set, otherwise DATABASE_URL.
2. The application DSN is the Neon pooled URL and backs the existing PostgreSQL stores.
3. Maintenance and migration flows prefer GOCLAW_POSTGRES_DSN_DIRECT, otherwise DATABASE_URL_UNPOOLED, then fall back to the application DSN for existing single-DSN deployments. If the selected endpoint hostname contains `-pooler`, reject it unless a direct URL is supplied. The pinned golang-migrate PostgreSQL driver uses a session-level advisory lock, which requires a stable PostgreSQL session.
4. Persistent GoClaw database records, including memory documents/chunks, sessions, context, episodic memory, knowledge graph, vault, and the rest of the PG-backed application stores, remain in Neon.
5. Koyeb injects PORT. GOCLAW_PORT remains an explicit override; otherwise config uses PORT and then the existing configured/default port.
6. The Docker health check probes the same effective port as the listener. The root Dockerfile embeds the web UI by default for source deployments and includes PostgreSQL 18 client binaries for built-in backup and restore.

## Configuration contract

- Keep database DSNs environment-only; do not add them to JSON config.
- Preserve GOCLAW_POSTGRES_DSN as the highest-priority application DSN.
- Add an explicit direct/migration DSN field without changing runtime pool configuration.
- Support Neon CLI's DATABASE_URL and DATABASE_URL_UNPOOLED names as fallbacks.
- Use the direct DSN for migrations and any database maintenance operation that relies on session-level behavior, including backup / restore paths that use pg_dump or pg_restore.
- Do not log either DSN or credentials.

## Koyeb deployment

Add a deployment guide for a WEB service built with the repository Dockerfile. It should specify an exposed HTTP port, route /, and /health; let Koyeb set PORT; preserve the Dockerfile entrypoint so schema upgrade runs; and configure database, gateway token, encryption key, and model provider values as Koyeb secrets. Koyeb builds a standard repository Dockerfile and exposes the configured service port. Sources: [Koyeb Dockerfile builds](https://www.koyeb.com/docs/build-and-deploy/build-from-git) and [Koyeb ports and PORT](https://www.koyeb.com/docs/build-and-deploy/exposing-your-service).

The GoClaw PG migration enables pgcrypto and pgvector. Neon documents pgvector support at [Neon pgvector](https://neon.com/docs/extensions/pgvector).

Explain the storage boundary: GoClaw database-backed memory and application data are durable in Neon, while uploaded media, arbitrary workspace files, runtime-installed packages, and skill content still use filesystem paths. Koyeb's container filesystem is not the Neon database. Unless the user steers otherwise, this task covers database-backed memory and records; persisting arbitrary files would require a separate Neon Object Storage implementation.

No Koyeb manifest will be added because current official Koyeb documentation describes Dockerfile builds and exposed-port settings through the control panel or CLI, but does not define a repository manifest format.

## Neon provisioning

Neon skills are installed for Codex and OpenCode and are recorded in skills-lock.json. The local Neon CLI profile check returned no authenticated account, and NEON_API_KEY is absent. The Neon skill's accountless path is Claimable Neon: it can create a temporary PostgreSQL project, but the user must claim it before its expiry to retain it. Do not start browser authentication. Use the claimable database only after the plan is approved, report its expiry / claim requirement, and never expose the connection URL in logs or tracked files. Object Storage is unavailable on unclaimed projects, and is available only in specified AWS regions; this is outside the recommended database-only scope.

After approval, Claimable Neon project `late-snow-76210001` was provisioned. The CLI wrote pooled/direct URLs to ignored `.env.local`, and `goclaw migrate up` completed at schema version 99. The project is still unclaimed and expires at `2026-10-18T16:24:38.847Z`; do not mint a claim code until the user is ready to open the 15-minute link.

## Compatibility and rollout

- Existing deployments using only GOCLAW_POSTGRES_DSN retain their current behavior.
- Existing non-Neon single-DSN deployments continue to migrate through that DSN as a fallback.
- Neon deployments must configure both the pooled and direct connection secrets; the direct URL is required for migration locking.
- Existing local SQLite records are not copied by selecting a new DSN. Treat data import as a separate migration requirement if the user asks to preserve those records.
- A freshly provisioned Neon project starts empty; existing PostgreSQL records also require an explicit import if they should be retained.
- Desktop SQLite remains local and is not part of the Koyeb server deployment.

## Surface parity

- Gateway/database: update DSN resolution for runtime stores, schema checks, upgrades, migration commands, and DB maintenance paths.
- API contract: N/A because response schemas remain unchanged.
- Web UI: N/A because memory/session APIs remain unchanged.
- CLI/runtime package: update migration/onboard DSN selection and deployment documentation; no command response schema changes.
