# Deployment Guide

## Overview

Production target uses a hybrid deployment:

- GoClaw gateway runs as a bare-metal `systemd` service.
- PostgreSQL 18 + pgvector runs in Docker.
- Nginx reverse proxies public HTTP/HTTPS traffic to GoClaw on localhost.
- Codex CLI is installed on the host for future agent-controlled CLI work.

Current VPS shape:

| Item | Value |
|---|---|
| Host | Private deployment value |
| SSH | Private deployment value |
| Domain | Private deployment value |
| Gateway listen | `127.0.0.1:18790` |
| Public proxy | Nginx on `80/443` |
| Postgres | Docker container `goclaw-postgres` |
| Postgres bind | `127.0.0.1:5432` |

Keep concrete hostnames, IPs, SSH users, SSH ports, and credentials in private operator notes or environment variables outside this public repository.

For command examples below:

```bash
export GOCLAW_HOST=<server-ip-or-hostname>
export GOCLAW_SSH_USER=<ssh-user>
export GOCLAW_SSH_PORT=<ssh-port>
export GOCLAW_DOMAIN=<public-domain>
```

## Server Layout

| Path | Purpose |
|---|---|
| `/opt/goclaw/releases/<release>` | Immutable GoClaw release directory |
| `/opt/goclaw/current` | Symlink to active release |
| `/opt/goclaw/shared/docker-compose.postgres.yml` | Postgres compose file |
| `/opt/goclaw/shared/postgres.env` | Postgres env file |
| `/opt/goclaw/backups/` | Uploaded DB backup files |
| `/etc/goclaw/config.json` | Gateway config |
| `/etc/goclaw/goclaw.env` | Gateway env and secrets |
| `/etc/goclaw/r2-backup.env` | R2 backup env and secrets |
| `/var/lib/goclaw/data` | GoClaw persistent data |
| `/var/lib/goclaw/workspace` | Agent workspace |
| `/var/lib/goclaw/postgres` | Postgres Docker data |
| `/usr/local/bin/goclaw-deploy` | Release switch, upgrade, health-check, rollback (source: [`scripts/zuey/goclaw-deploy.sh`](../scripts/zuey/goclaw-deploy.sh)) |
| `/usr/local/bin/goclaw-issue-ssl` | Certbot wrapper for the deployment domain |
| `/usr/local/bin/goclaw-backup-r2` | Postgres dump, R2 upload, retention cleanup |
| `/usr/local/bin/goclaw-upgrade-release` | Download and deploy a GitHub Release tarball (source: [`scripts/zuey/goclaw-upgrade-release.sh`](../scripts/zuey/goclaw-upgrade-release.sh)) |

Secrets are stored only in server env files. Do not copy tokens or database passwords into repo docs.

## Runtime Services

Check status:

```bash
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST"
systemctl status goclaw --no-pager
systemctl status nginx --no-pager
systemctl status goclaw-backup-r2.timer --no-pager
docker ps --filter name=goclaw-postgres
curl -fsS http://127.0.0.1:18790/health
```

Expected health:

```json
{"status":"ok","protocol":3}
```

## Initial Deployment Record

Completed on 2026-05-17:

- Installed Docker, Docker Compose v2, Nginx, Certbot, Node.js 22, Codex CLI.
- Added the operator workstation SSH public key to the deployment user.
- Installed Codex CLI. For agent-controlled `codex exec`, authenticate the Linux service user that runs GoClaw (`goclaw`), not only the SSH operator user.
- Restored the latest private PostgreSQL backup into Docker Postgres.
- Upgraded restored schema from `57` to `65`.
- Started the initial GoClaw release by `systemd`.
- Added automated database backup to a private Cloudflare R2 bucket.
- Verified local and Nginx health endpoints.

Current verification snapshot:

```text
goclaw_active=active
nginx_active=active
health_local={"status":"ok","protocol":3}
health_nginx={"status":"ok","protocol":3}
docker=goclaw-postgres pgvector/pgvector:pg18 healthy
schema=65
codex=codex-cli 0.130.0
```

## Codex CLI Auth For Agents

The gateway runs under the `goclaw` Linux user. Codex auth is stored under the
effective home directory, so an SSH-session login such as `codex login
--device-auth` under an operator user only writes that user's `~/.codex/auth.json`.
Agents invoking `codex` through the exec tool use the service user's home:

```text
/var/lib/goclaw/.codex/auth.json
```

Verify both contexts when debugging auth:

```bash
codex login status
sudo -u goclaw -H codex login status
```

If the operator user is logged in but `goclaw` is not, either run device auth as
the service user or copy the operator auth file with strict ownership:

```bash
sudo install -d -o goclaw -g goclaw -m 700 /var/lib/goclaw/.codex
sudo install -o goclaw -g goclaw -m 600 ~/.codex/auth.json /var/lib/goclaw/.codex/auth.json
sudo -u goclaw -H codex login status
sudo -u goclaw -H sh -lc 'mkdir -p /var/lib/goclaw/codex-smoke && cd /var/lib/goclaw/codex-smoke && codex exec --skip-git-repo-check --sandbox read-only "Reply with exactly: CODEX_AUTH_OK"'
```

Do not store Codex auth material in repository docs. Treat `auth.json` as a
credential.

## DNS And SSL

Cloudflare DNS record:

| Type | Name | Value | Proxy |
|---|---|---|---|
| `A` | Deployment subdomain | Deployment host IP | Proxied |

SSL was issued with Certbot for the deployment domain on 2026-05-17 and Certbot installed automatic renewal.

Verify HTTPS:

```bash
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST"
curl -fsS "https://$GOCLAW_DOMAIN/health"
```

Re-issue manually if needed:

```bash
sudo /usr/local/bin/goclaw-issue-ssl
```

## Deploy A New Release

Preferred server-side upgrade flow:

```bash
sudo /usr/local/bin/goclaw-upgrade-release --dry-run latest
sudo /usr/local/bin/goclaw-upgrade-release latest
sudo /usr/local/bin/goclaw-upgrade-release v3.12.0
```

The script downloads the Linux amd64 GitHub Release tarball from `digitopvn/goclaw`, follows GitHub release redirects, verifies `CHECKSUMS.sha256` when present, falls back to the GitHub release asset SHA256 digest for beta assets without checksum files, extracts to `/opt/goclaw/releases/<tag>`, and calls `goclaw-deploy`. When invoked from the running gateway service, it first re-launches itself as a transient `systemd-run` unit so `goclaw-deploy` can stop/restart `goclaw` without killing the upgrade job.

The HTTP API still accepts only `tag`; it does not accept repo names or custom download URLs.

Remote API trigger is available in builds that include the gateway upgrade endpoint:

```bash
curl -fsS -X POST "https://$GOCLAW_DOMAIN/v1/system/gateway/upgrade" \
  -H "Authorization: Bearer <gateway-token>" \
  -H "X-GoClaw-Upgrade-Token: <upgrade-token>" \
  -H "Content-Type: application/json" \
  --data '{"tag":"latest"}'
```

Check status:

```bash
curl -fsS "https://$GOCLAW_DOMAIN/v1/system/gateway/upgrade/status" \
  -H "Authorization: Bearer <gateway-token>" \
  -H "X-GoClaw-Upgrade-Token: <upgrade-token>"
```

Keep upgrade tokens in server env files or secret managers. Do not put real tokens in docs.

The remote trigger endpoint fails closed unless `GOCLAW_UPGRADE_TRIGGER_TOKEN` is configured in the gateway environment.

### Automatic Beta Deploy From `dev`

Pushing or merging into `dev` runs `.github/workflows/dev-beta-release.yaml`. After Go/Web checks pass, the workflow creates the next semantic beta tag, publishes the linux amd64 prerelease asset and checksum, then deploys that exact beta tag to the zuey VPS through the gateway upgrade endpoint.

Linux arm64 release assets, full checksums, multi-arch Docker images, and beta Docker aliases continue after the fast zuey deploy path. Release asset completion waits until zuey deploy finishes before using `--clobber`, so the VPS upgrade script cannot race with a release asset refresh while downloading the linux amd64 tarball. Those completion jobs are still required to pass; the workflow remains failed if later artifact completion breaks. Zuey deploy and Docker beta alias promotion both skip stale beta tags so older runs cannot roll back a newer beta.

Required GitHub Actions configuration:

| Name | Type | Value |
|---|---|---|
| `ZUEY_GOCLAW_URL` | Secret | Public gateway URL, for example `https://goclaw.zuey.me` |
| `ZUEY_GOCLAW_GATEWAY_TOKEN` | Secret | Gateway bearer token from the server env |
| `ZUEY_GOCLAW_UPGRADE_TOKEN` | Secret | Upgrade trigger token from the server env |
| `ZUEY_GOCLAW_USER_ID` | Variable | Optional owner identity, defaults to `system` |

The deploy job sends:

```bash
POST /v1/system/gateway/upgrade {"tag":"vX.Y.Z-beta.N"}
GET /v1/system/gateway/upgrade/status
GET /health
```

The workflow fails if the upgrade status becomes `failed`, times out, or public health does not return `{"status":"ok"}`.

Manual local-build fallback:

Build locally with embedded web UI:

```bash
cd ui/web
pnpm install --frozen-lockfile
pnpm build
cd ../..
rm -rf internal/webui/dist
mkdir -p internal/webui/dist
cp -r ui/web/dist/* internal/webui/dist/
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -tags embedui \
  -ldflags="-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=<version>" \
  -o dist/goclaw-linux-amd64 .
```

Upload:

```bash
release=<version-or-date>
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST" "mkdir -p /opt/goclaw/releases/$release/migrations"
scp -P "$GOCLAW_SSH_PORT" dist/goclaw-linux-amd64 "$GOCLAW_SSH_USER@$GOCLAW_HOST:/opt/goclaw/releases/$release/goclaw"
scp -P "$GOCLAW_SSH_PORT" migrations/* "$GOCLAW_SSH_USER@$GOCLAW_HOST:/opt/goclaw/releases/$release/migrations/"
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST" "chmod +x /opt/goclaw/releases/$release/goclaw && sudo /usr/local/bin/goclaw-deploy /opt/goclaw/releases/$release"
```

`goclaw-deploy` does:

1. Validate binary and migrations.
2. Switch `/opt/goclaw/current`.
3. Run `goclaw upgrade`.
4. Restart `goclaw`.
5. Poll `/health`.
6. Roll back symlink and restart if health fails.

### Self-loop symlink guard

`goclaw-deploy` captures the previous release for rollback via `readlink -f /opt/goclaw/current` before swinging the symlink. If `/opt/goclaw/current` is ever a self-loop (e.g. `current -> current`) or otherwise unresolvable, `readlink -f` exits non-zero. Combined with `set -euo pipefail`, an unguarded call aborts the script before `ln -sfn` runs, so deploys fail silently and zero rollback target is recorded — observed in production on 2026-05-27 where every `deploy_zuey_beta` CI run failed at this step.

The script swallows the readlink failure (`readlink -f ... 2>/dev/null || true`) and logs a warning when the symlink exists but resolves to empty, then proceeds to overwrite. Rollback is skipped in that case because no valid `previous` is available.

If you ever edit `/usr/local/bin/goclaw-deploy` on zuey, preserve this guard:

```bash
previous=""
if [ -L /opt/goclaw/current ]; then previous="$(readlink -f /opt/goclaw/current 2>/dev/null || true)"; fi
if [ -z "$previous" ] && [ -L /opt/goclaw/current ]; then
  echo "warn: /opt/goclaw/current is a symlink but readlink -f failed (likely self-loop or broken target); overwriting without rollback target" >&2
fi
```

The canonical source of this script lives in the repo at [`scripts/zuey/goclaw-deploy.sh`](../scripts/zuey/goclaw-deploy.sh), alongside [`scripts/zuey/goclaw-upgrade-release.sh`](../scripts/zuey/goclaw-upgrade-release.sh). The VPS copies at `/usr/local/bin/goclaw-deploy` and `/usr/local/bin/goclaw-upgrade-release` are downstream replicas. CI auto-syncs both on every beta release via the `Sync zuey ops scripts to VPS` step in `.github/workflows/dev-beta-release.yaml`. For manual sync (off-CI):

```bash
scp -P 2233 scripts/zuey/goclaw-deploy.sh scripts/zuey/goclaw-upgrade-release.sh \
  zuey@82.197.71.246:/tmp/
ssh -p 2233 zuey@82.197.71.246 'bash -s' <<'EOF'
set -euo pipefail
ts=$(date +%Y%m%d-%H%M%S)
for name in goclaw-deploy goclaw-upgrade-release; do
  if [ -f "/usr/local/bin/$name" ]; then
    sudo cp -p "/usr/local/bin/$name" "/usr/local/bin/${name}.bak-${ts}"
  fi
  sudo install -o root -g root -m 0755 "/tmp/${name}.sh" "/usr/local/bin/$name"
  sudo bash -n "/usr/local/bin/$name" && echo "OK: $name"
done
EOF
```

Always back up the live copy before overwriting (the `cp -p` line above does this).

### CI auto-sync — required GitHub secrets

The `Sync zuey ops scripts to VPS` step in `dev-beta-release.yaml` runs `scp + sudo install` before triggering the gateway upgrade endpoint. It needs the following repository secrets configured under **Settings → Secrets and variables → Actions**:

| Secret | Purpose |
|---|---|
| `ZUEY_SSH_PRIVATE_KEY_B64` | CI-only ed25519/rsa key, **base64-encoded as a single line** (`base64 -w0 < /path/to/key`). Its public key must be appended to `zuey@82.197.71.246:~/.ssh/authorized_keys`. **Do not reuse the operator's personal key.** Base64 avoids the `error in libcrypto` failure caused by GitHub Secrets normalizing newlines inside multi-line PEM blocks. |
| `ZUEY_SUDO_PASS` | Same value as `ZUEY_GOCLAW_SUDO_PASS` in the operator's local `.env`; used by `sudo -S` over the SSH session to install scripts. |

Optional repository **variables** (override defaults if the VPS endpoint changes):

| Variable | Default |
|---|---|
| `ZUEY_SSH_HOST` | `82.197.71.246` |
| `ZUEY_SSH_PORT` | `2233` |
| `ZUEY_SSH_USER` | `zuey` |

To rotate the CI SSH key:

```bash
ssh-keygen -t ed25519 -f /tmp/ci-zuey-key -N '' -C 'gh-actions-deploy-zuey-beta'
# add /tmp/ci-zuey-key.pub to zuey:~/.ssh/authorized_keys (consider restricting to scp+install via `command="..."` forced-command)
# base64-encode the private key on a single line, then paste into the
# ZUEY_SSH_PRIVATE_KEY_B64 secret:
base64 -w0 < /tmp/ci-zuey-key  # macOS: `base64 -i /tmp/ci-zuey-key | tr -d '\n'`
# (copy the single-line output and paste it into the secret value)
# then `shred -u /tmp/ci-zuey-key*` locally
```

## Backup And Restore

Automated backups:

| Item | Value |
|---|---|
| Timer | `goclaw-backup-r2.timer` |
| Schedule | Every 6 hours: `00:00`, `06:00`, `12:00`, `18:00` server time |
| Source | Docker Postgres container `goclaw-postgres` |
| Format | PostgreSQL custom dump, `pg_dump -Fc` |
| Local directory | `/opt/goclaw/backups/` |
| R2 bucket | Private deployment value |
| R2 prefix | Private deployment value |
| Retention | Keep latest 20 backups locally and in R2 |

Check timer and latest logs:

```bash
systemctl list-timers goclaw-backup-r2.timer --no-pager
journalctl -u goclaw-backup-r2.service -n 80 --no-pager
```

Run a manual backup:

```bash
sudo systemctl start goclaw-backup-r2.service
```

Create a database dump on server:

```bash
ts=$(date +%Y%m%d-%H%M%S)
docker exec goclaw-postgres pg_dump -U goclaw -Fc -d goclaw > /opt/goclaw/backups/goclaw-$ts.dump
```

Restore a dump:

```bash
systemctl stop goclaw
docker exec -i goclaw-postgres pg_restore -U goclaw -d goclaw --clean --if-exists --no-owner < /opt/goclaw/backups/<file>.dump
sudo /usr/local/bin/goclaw-deploy /opt/goclaw/current
```

## Operational Notes

- Gateway runs as Linux user `goclaw`.
- Host-control exceptions are deployment-specific and must be documented privately, not in this public runbook.
- Workspace restriction settings are deployment-specific and must be reviewed before enabling agent-controlled host operations.
- Postgres is bound only to localhost.
- UFW allows the private SSH port, `80/tcp`, and `443/tcp`.
- Reboot is recommended later because the VPS reports a pending kernel upgrade.

## Troubleshooting

Logs:

```bash
journalctl -u goclaw -n 200 --no-pager
docker logs goclaw-postgres --tail 100
tail -n 100 /var/log/nginx/error.log
```

Restart:

```bash
systemctl restart goclaw
docker compose --env-file /opt/goclaw/shared/postgres.env -f /opt/goclaw/shared/docker-compose.postgres.yml restart postgres
systemctl reload nginx
```

Unresolved questions: none.

## Koyeb With Neon

This is an alternative server deployment using the repository's root Dockerfile and a Neon PostgreSQL project.

### Neon database

Create a PostgreSQL project in Neon and copy both connection strings from its connection details:

| Environment variable | Neon connection | Use |
|---|---|---|
| `DATABASE_URL` | Pooled connection string (`-pooler` hostname) | GoClaw runtime queries and stores |
| `DATABASE_URL_UNPOOLED` | Direct connection string | Required for Neon schema migrations and startup schema checks; use for database backup/restore |

Set both URLs for Neon. GoClaw's PostgreSQL migration driver takes a session-level advisory lock, so sending migrations through Neon's transaction pooler can break the lock; `DATABASE_URL_UNPOOLED` supplies the direct connection for migrations. GoClaw rejects a `-pooler` URL when no direct URL is configured. The application still falls back to its runtime URL for existing single-DSN deployments using a direct endpoint. The database credentials stay in Koyeb secrets. Do not put either connection string in `config.json`, `.env.example`, or a committed deployment file. Existing deployments can keep using `GOCLAW_POSTGRES_DSN`; it takes precedence over `DATABASE_URL`. `GOCLAW_POSTGRES_DSN_DIRECT` takes precedence over `DATABASE_URL_UNPOOLED`.

GoClaw runs the SQL migrations on container startup through the existing entrypoint. The initial PostgreSQL migration enables `pgcrypto` and `vector`; Neon supports the `pgvector` extension used by GoClaw.

### Koyeb service

1. Create a **Web Service** from the GoClaw Git repository and build it with the root `Dockerfile`.
2. Configure an HTTP exposed port of `18790` and route `/` to the service. Koyeb supplies that port through `PORT`; leave `GOCLAW_PORT` unset unless you need to override it.
3. Configure an HTTP health check for `/health` on the exposed port.
4. Keep the Dockerfile entrypoint and default `serve` command so database upgrades run before the gateway starts.
5. Add the following as Koyeb secrets/environment variables:

   | Variable | Required | Purpose |
   |---|---:|---|
   | `DATABASE_URL` | Yes | Neon pooled runtime connection |
   | `DATABASE_URL_UNPOOLED` | Yes | Neon direct connection for migrations and maintenance |
   | `GOCLAW_GATEWAY_TOKEN` | Yes | Gateway authentication token |
   | `GOCLAW_ENCRYPTION_KEY` | Yes | Encrypt provider credentials stored in PostgreSQL |
   | `GOCLAW_PROVIDER` / `GOCLAW_MODEL` | Optional | Default provider and model |
   | `GOCLAW_<PROVIDER>_API_KEY` | Optional | Provider API key when configuring it through environment variables |

The root Dockerfile embeds the Web UI by default and includes PostgreSQL 18 client utilities used by built-in database backup and restore. Set `ENABLE_EMBEDUI=false` only when building a backend-only image. Koyeb's Dockerfile build and port settings are described in [Build from Git](https://www.koyeb.com/docs/build-and-deploy/build-from-git) and [Exposing your Service](https://www.koyeb.com/docs/build-and-deploy/exposing-your-service).

### Koyeb Free instance

Koyeb's Free instance is a Web Service with **0.1 vCPU, 512 MB RAM, and 2 GB SSD**. Important limits:

- One Free instance per organization; Web Services only.
- Region must be **Washington, D.C.** or **Frankfurt**. Choose Washington, D.C. when the Neon project is in `aws-us-east-2` (Ohio) to keep database latency low.
- No custom scaling, no Volumes, and no persistent container storage.
- The instance **scales down to zero after 1 hour without traffic**. The next request triggers a cold start (image boot, schema check, gateway start) and takes noticeably longer than a warm request; WebSocket clients must reconnect. Scheduled cron/heartbeat work does not run while the instance is asleep.
- Koyeb documents Free instances as unsuitable for production. Use them for demos, testing, and personal use.

Service settings for the Free instance:

| Setting | Value |
|---|---|
| Service type | Web Service |
| Repository / branch | fork or upstream, branch that contains this guide (e.g. `dev`) |
| Builder | Dockerfile (repository root) |
| Instance type | Free |
| Region | Washington, D.C. |
| Port | `18790` (HTTP) |
| Health check | HTTP `GET /health`, grace period `300` seconds (Koyeb allows 5–900) |
| Restart limit | default |
| Scaling | not configurable on Free |

Add these environment variables/secrets in addition to the required secrets above. They keep the Go runtime and database pool inside the 512 MB / 0.1 vCPU budget:

| Variable | Recommended value | Purpose |
|---|---|---|
| `GOMEMLIMIT` | `400MiB` | Bounds the Go heap below the 512 MB instance cap |
| `GOMAXPROCS` | `1` | Avoids scheduler/GC threads sized for host CPUs under a 0.1 vCPU quota |
| `GOCLAW_PG_MAX_OPEN_CONNS` | `5` | Smaller Neon pool for a single tiny instance |
| `GOCLAW_PG_MAX_IDLE_CONNS` | `2` | Fewer idle DB connections |
| `GOCLAW_ALLOWED_ORIGINS` | `https://<service>.koyeb.app` | Restricts WebSocket CORS to the service origin; without it the gateway logs `security.cors_open` and accepts all origins |

Leave `GOCLAW_PORT` unset so Koyeb's injected `PORT` selects the listener, and set the health check grace period high enough for the cold start plus startup migrations; 300 seconds is ample.

### Agnes AI provider (OpenAI-compatible)

Agnes AI exposes an OpenAI-compatible API with streaming and tool calling. Add it in the web dashboard after the service is healthy:

1. Open the service URL and sign in with `GOCLAW_GATEWAY_TOKEN`.
2. Providers → Add provider (or the setup wizard): type **OpenAI Compatible** (`openai_compat`), API base `https://apihub.agnes-ai.com/v1`, API key from the Agnes console, and model `agnes-2.5-flash`.
3. Optionally set it as the default provider/model so new agents use it automatically. Provider credentials are stored encrypted using `GOCLAW_ENCRYPTION_KEY`; keep the API key out of tracked files.

Agnes AI does not document an embeddings endpoint, so semantic memory and knowledge-graph embeddings stay disabled unless a separate embeddings provider is configured. Without embeddings, memory features that need vectors are degraded, not broken.

### Persistent and ephemeral data

Memory documents, conversation state, agent context, episodic memory, knowledge graph, vault records, and the other PostgreSQL-backed application data are stored in Neon. Uploaded media, arbitrary workspace files, runtime-installed packages, and skill content still use local filesystem paths; Koyeb container storage does not make those files durable. Persisting those files requires a separate object-storage implementation.

Neon connection pooling uses PgBouncer transaction mode. GoClaw sends normal runtime queries through `DATABASE_URL` and sends migrations and database dump/restore operations through `DATABASE_URL_UNPOOLED`. Existing non-Neon single-DSN deployments retain the application-URL fallback.

A newly created Neon project starts with an empty GoClaw schema. Pointing GoClaw at it does not import records from an existing PostgreSQL or desktop SQLite database; moving existing records requires a separate export/import step.
