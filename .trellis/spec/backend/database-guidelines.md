# Database Guidelines

GoClaw uses raw SQL through `database/sql`, `sqlx`, and pgx; it has no ORM. PostgreSQL is the standard server database. The desktop/Lite edition uses a separate SQLite implementation and migration system.

## Connections and secrets

- `internal/config.DatabaseConfig` keeps database DSNs out of JSON (`json:"-"`). Read secrets from environment variables only.
- `GOCLAW_POSTGRES_DSN` is the application connection and takes precedence over `DATABASE_URL`. Keep the legacy name working for existing deployments.
- `GOCLAW_POSTGRES_DSN_DIRECT` takes precedence over `DATABASE_URL_UNPOOLED`. `DatabaseConfig.MigrationDSN()` selects this direct connection when present, then falls back only to a direct application DSN for legacy one-URL deployments.
- Use the pooled application DSN for normal PostgreSQL store traffic. Use the direct DSN for schema migrations and maintenance operations such as database dumps/restores. Neon pooled connections use PgBouncer transaction mode.
- Never log a DSN or write one into `config.json`, tracked environment examples, or deployment manifests.

## Queries and stores

- PostgreSQL stores are constructed in `internal/store/pg/factory.go` and share the application pool. Keep user values parameterized with PostgreSQL `$1`, `$2`, etc.
- SQLite stores use `?` parameters and must remain compatible with the SQLite-only build. Do not route Lite's local database through Neon.
- Reuse entities already loaded in the current request or pipeline instead of issuing duplicate queries. Apply tenant and agent scopes in SQL as well as request authorization.

## Migrations

- PostgreSQL schema changes belong in `migrations/` and use `golang-migrate`. Startup compatibility checks and the `migrate` / `upgrade` commands should use `MigrationDSN()`.
- SQLite has independent full-schema and incremental migration sources. When a schema change affects SQLite, update `internal/store/sqlitestore/schema.sql`, add an incremental patch in `schema.go`, and bump its schema version.
- PostgreSQL and SQLite schemas can differ only when the feature is intentionally unavailable in Lite. Document that boundary.

## Common mistakes

- Sending a migration or `pg_dump` through a transaction pooler can break session-dependent operations. Select the direct DSN for maintenance.
- Supporting `DATABASE_URL` must not weaken the existing GoClaw variable's precedence or make secrets serializable in config.
- A durable Neon database does not make container filesystem uploads, workspaces, or skill files durable.

## Scenario: Neon PostgreSQL and Koyeb

### 1. Scope / Trigger

Use this contract when connecting the standard server to Neon or another PostgreSQL platform that provides pooled and direct connection URLs, and when deploying the Docker image to a host that injects `PORT`.

### 2. Signatures

- `config.DatabaseConfig.PostgresDSN`: pooled/application connection.
- `config.DatabaseConfig.PostgresDSNDirect`: direct maintenance connection.
- `config.DatabaseConfig.MigrationDSN() (string, error)`: direct URL when set; otherwise a direct application URL. It returns a configuration error when the selected application or direct URL hostname contains `-pooler` without an unpooled URL.
- `cmd.checkSchemaOrAutoUpgrade(dsn string)`: schema check and optional migration path; pass `MigrationDSN()`.

### 3. Contracts

| Variable | Precedence | Consumer |
|---|---|---|
| `GOCLAW_POSTGRES_DSN` | First application URL | PostgreSQL stores and normal queries |
| `DATABASE_URL` | Fallback application URL | Same |
| `GOCLAW_POSTGRES_DSN_DIRECT` | First direct URL | Migrations and backup/restore maintenance |
| `DATABASE_URL_UNPOOLED` | Fallback direct URL | Same |
| `GOCLAW_PORT` | First valid port | Listener and Docker health check |
| `PORT` | Fallback valid port | Koyeb listener and health check |

All DSNs are environment-only (`json:"-"`). If no direct DSN is set, migration and maintenance commands fall back to a direct application DSN for compatibility with existing single-DSN deployments. A URL with `-pooler` in its hostname is rejected unless a direct URL is configured.

Koyeb Free constraints: the Free tier allows one instance per organization (0.1 vCPU / 512 MB / 2 GB SSD, Web Services only, region limited to Washington, D.C. or Frankfurt, no Volumes, scale-to-zero after one hour idle) and is documented as unsuitable for production. On that tier set `GOMEMLIMIT=400MiB`, `GOMAXPROCS=1`, `GOCLAW_PG_MAX_OPEN_CONNS=5`, and `GOCLAW_PG_MAX_IDLE_CONNS=2`, leave `GOCLAW_PORT` unset, and give the `/health` HTTP probe a 300-second grace period so cold start plus startup migrations can finish. Set `GOCLAW_ALLOWED_ORIGINS` to the public service origin to lock WebSocket CORS; without it the gateway logs `security.cors_open` and accepts every origin. Neon deployments must set both URLs: the pinned `golang-migrate` PostgreSQL driver acquires a session-level advisory lock, so migrations must use Neon's direct URL instead of the transaction pooler. The Docker image leaves `GOCLAW_PORT` unset by default, embeds the Web UI by default, includes PostgreSQL client utilities for built-in DB backup/restore, and retains the entrypoint that runs upgrades before serving.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
|---|---|
| Neither application DSN is set | Server reports PostgreSQL is not configured and exits |
| Direct application DSN is set; direct DSN is absent | Runtime and migrations use the application DSN |
| Both pooled and direct DSNs are set | Stores use pooled; migrations and maintenance use direct |
| Application URL hostname contains `-pooler`, with no direct URL | `MigrationDSN()` returns a configuration error before schema checks or migrations; set `DATABASE_URL_UNPOOLED` |
| Configured direct URL hostname contains `-pooler` | `MigrationDSN()` returns a configuration error; use the unpooled direct URL |
| Both `GOCLAW_PORT` and `PORT` contain valid ports | `GOCLAW_PORT` wins |
| `GOCLAW_PORT` is invalid and `PORT` is valid | `PORT` wins |
| No valid port environment value exists | Existing config/default port remains |

### 5. Good/Base/Bad Cases

- Good: set `DATABASE_URL` to Neon's pooled URL and `DATABASE_URL_UNPOOLED` to its direct URL in platform secrets.
- Base: an existing non-Neon deployment sets only `GOCLAW_POSTGRES_DSN`; behavior remains single-URL and backward compatible.
- Bad: pass a `-pooler` URL to `pg_dump`, `psql`, or a session-dependent migration operation.

### 6. Tests Required

Future changes to this contract should assert: environment precedence for both DSN fields; direct-DSN selection and direct single-DSN fallback through `MigrationDSN()`; rejection of pooled URLs without a direct URL; runtime store construction receives `PostgresDSN`; migration/startup and backup/restore paths receive the resolved maintenance DSN; and port precedence `GOCLAW_PORT` → `PORT` → config default. Keep SQLite-only behavior covered when changing shared configuration.

### 7. Wrong vs Correct

#### Wrong

```go
maintenanceDSN, err := cfg.Database.MigrationDSN()
if err != nil {
	return err
}
store.NewPGStores(store.StoreConfig{PostgresDSN: maintenanceDSN})
```

This routes normal store traffic through the direct endpoint even when a pooled runtime URL is configured.

#### Correct

```go
store.NewPGStores(store.StoreConfig{PostgresDSN: cfg.Database.PostgresDSN})
maintenanceDSN, err := cfg.Database.MigrationDSN()
if err != nil {
	return err
}
if err := checkSchemaOrAutoUpgrade(maintenanceDSN); err != nil {
	return err
}
```

Normal queries use the application DSN; schema maintenance selects the direct DSN and preserves the legacy single-DSN fallback.
