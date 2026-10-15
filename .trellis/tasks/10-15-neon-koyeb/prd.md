# Move GoClaw memory persistence to Neon and prepare Koyeb deployment

## Goal

Run GoClaw's standard server edition on Koyeb with Neon PostgreSQL as the durable database for agent memory and conversation state.

## Confirmed facts

- The default server build requires PostgreSQL. The gateway store setup creates PostgreSQL-backed stores; the PG factory wires session, memory, agent context, episodic memory, knowledge graph, and vault stores to PostgreSQL.
- The memory interceptor routes MEMORY.md and memory/*.md operations to the memory store. The context file interceptor routes agent context files to the agent store.
- Neon provides a pooled connection for application traffic and a direct connection for schema migrations. Neon CLI uses DATABASE_URL and DATABASE_URL_UNPOOLED for those values.
- GoClaw currently reads GOCLAW_POSTGRES_DSN, not DATABASE_URL, and uses the same DSN for runtime queries and migrations.
- The Docker image exposes port 18790 and has a health check fixed to port 18790. The gateway already reads GOCLAW_PORT; it does not currently use Koyeb's PORT.
- A source build defaults ENABLE_EMBEDUI to false, while the standard published image is built with the web UI embedded.
- Neon skills were installed for Codex and OpenCode. skills-lock.json records the project skill install.
- No Neon API key or authenticated Neon CLI profile is configured in this environment. Neon Claimable can create a temporary project without an account, but a human must claim it before its expiry to keep it.

## Requirements

- Use Neon PostgreSQL for persistent server-side memory and conversation data.
- Accept Neon's standard DATABASE_URL pooled variable and DATABASE_URL_UNPOOLED direct variable while preserving the existing GoClaw DSN variables.
- Use the pooled DSN for normal store traffic and require a direct DSN for migrations and schema compatibility checks when the application DSN uses a transaction pooler. Existing direct single-DSN deployments retain the fallback.
- Require both pooled and direct Neon URLs in the Koyeb setup. Neon migrations use a session-level advisory lock, which must not be routed through Neon's transaction pooler. Preserve the one-DSN fallback for existing non-Neon deployments.
- Keep all database credentials in environment variables / platform secrets; never put them in tracked config or deployment files.
- Make the Docker image honor Koyeb's injected PORT and keep its health check aligned with the selected port.
- Make a Koyeb source build serve the embedded web UI by default and document the required Koyeb settings and secrets.
- Preserve support for existing deployments that only set GOCLAW_POSTGRES_DSN to a direct, non-pooled PostgreSQL endpoint.
- Use Neon PostgreSQL only for this server deployment; desktop SQLite remains a separate local edition.

## Acceptance Criteria

- [x] The server uses DATABASE_URL when GOCLAW_POSTGRES_DSN is absent, and the legacy variable takes precedence when both exist.
- [x] Migrations use DATABASE_URL_UNPOOLED (or the explicit GoClaw direct-DSN variable) when configured, with a backward-compatible fallback only for a direct, non-pooled application DSN.
- [x] A pooled `-pooler` URL without a direct DSN fails with a clear configuration error before schema checks or migrations run.
- [x] Existing PostgreSQL-backed memory, sessions, context files, episodic memory, knowledge graph, and vault records persist in the configured Neon database.
- [x] Koyeb's PORT controls the listener when GOCLAW_PORT is not explicitly set, and the Docker health check probes the same port.
- [x] Building the root Dockerfile without extra flags embeds the web UI.
- [x] The standard runtime image includes `pg_dump` and `psql` for built-in database backup and restore.
- [x] A deployment guide explains Neon setup, the Koyeb Dockerfile service, required runtime secrets, exposed port / health check, schema migration, and the limits of ephemeral local files.
- [x] The deployment guide states that a new Neon project starts empty and that existing PostgreSQL or desktop SQLite records are not imported automatically.
- [x] No Neon, database, gateway, or provider secret is added to tracked files.

## Approved scope

The user approved database-backed memory, conversation state, context files, and application records in Neon. Uploads, arbitrary workspace files, and installed skill content remain filesystem-backed; Neon Object Storage is not part of this task.

## Out of scope

- Replacing GoClaw's PostgreSQL store layer or changing the memory API.
- Moving desktop/Lite SQLite data automatically unless requested separately.
- Deploying the service to a Koyeb account; this repository task prepares the source and deployment instructions.

## Neon provisioning result

The Neon CLI had no authenticated profile, so the approved accountless Claimable path created project `late-snow-76210001`. Its pooled/direct URLs are in ignored `.env.local`; migrations were applied successfully through schema version 99. The project remains unclaimed, expires at `2026-10-18T16:24:38.847Z`, and needs the user to claim it to keep it. Do not mint a claim code until the user is ready to open the 15-minute link.
