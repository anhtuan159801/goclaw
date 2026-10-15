# Implementation plan

## Approved scope

- The user approved Neon persistence for database-backed memory and application data, plus Koyeb deployment preparation.
- Uploaded media, arbitrary workspace files, runtime-installed packages, and skill content remain filesystem-backed and are outside this task.

## Ordered work

1. [x] Keep file/object persistence out of scope unless the user expands it; Claimable Neon cannot provide Object Storage before claim.
2. [x] Accept DATABASE_URL and DATABASE_URL_UNPOOLED while preserving GoClaw-specific DSN precedence and environment-only secrets; reject transaction-pooler URLs for migration flows unless a direct URL is set.
3. [x] Route normal stores through the application DSN and migration, schema, backup, and restore operations through the direct DSN with fallback for existing direct single-DSN deployments.
4. [x] Resolve ports as GOCLAW_PORT, then PORT, then config/default; remove the Dockerfile port override, align the health check, embed the web UI by default, and install PostgreSQL client binaries for container backup/restore.
5. [x] Extend the deployment guide with required Neon pooled/direct secrets, why migrations require a direct URL, Koyeb service settings, health path, startup migrations, filesystem durability boundaries, and the empty-database import boundary.
6. [x] Add the verified DSN and deployment contracts to the backend database spec.
7. [x] Install Neon skills, create claimable project `late-snow-76210001`, and apply migrations through schema version 99. Connection URLs are in ignored `.env.local`. Project remains unclaimed and expires at `2026-10-18T16:24:38.847Z`; do not mint the 15-minute claim code until the user is ready to open it.
8. [x] Run formatting and the requested non-test checks. Both Go builds pass; targeted vet passes. `go fix ./...` applied Go 1.26 upgrades and reported the same Windows test-file build-tag issue as full vet (`document_parser_test.go` refers to `waitForRecordedPIDs` from `shell_abort_test.go`, which is excluded on Windows). Docker is unavailable and the available `bash.exe` is only WSL with no installed distribution, so Dockerfile build and shell syntax checks could not run. No test suite was run. `goclaw doctor` using only `DATABASE_URL` connected through Neon pooling and reported schema v99 up to date with all hooks applied.

## High-risk points

- Runtime stores must stay on the pooled DSN while migration and maintenance operations use the direct DSN when available.
- Every DSN consumer must be classified; don't accidentally send backup/restore through the Neon transaction pooler.
- Docker ENV values can shadow Koyeb's injected PORT; ensure the listener and health check use the same resolved port.
- A Claimable Neon project has a limited unclaimed lifetime. Do not describe it as permanent and do not claim it on the user's behalf.
- Koyeb local filesystem is ephemeral. Do not claim arbitrary uploads or workspace files are durable unless a cloud file store is implemented.
