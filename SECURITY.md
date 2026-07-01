# SECURITY.md — Production Security Posture

This document tracks production security findings, the
investigations that confirmed or ruled them out, and the fixes
that were applied. If a future operator sees a similar alert,
start here.

---

## 2026-07-01 — optitoken-postgres exposed to the public internet

### Alert received
> "PostgreSQL is a relational database management system (DBMS)
> often used with web applications. Unauthorized access to the
> DBMS by exploiting vulnerabilities, misconfigurations, or by
> using compromised login credentials can result in malicious
> actors being able to access, manipulate or delete information
> stored in the databases, which can have far-reaching
> consequences. To protect against such kind of attacks, access
> to the DBMS should be limited to the application server and
> trusted management networks or a VPN connection. The DBMS
> should never be exposed to the Internet."

### Pre-existing condition, NOT a regression I caused
The `docker-compose.prod.yml` file has had the line

```yaml
    ports:
      - "5432:5432"
```

under the `postgres:` service since before any of my recent
L3 / translator / telemetry / cache fixes. An automated
external scanner picked this up on 2026-07-01 and emailed
the alert.

### Read-only investigation (NO config changes yet)

I ran read-only queries against the running PostgreSQL container
to look for evidence of compromise. All queries used the
existing service. Nothing was modified.

**Active connections (`pg_stat_activity`):**

| Source | Count | State |
|---|---|---|
| `172.18.0.4` (proxy) | 2 | idle |
| `172.18.0.5` (dashboard) | 4 | idle |
| local psql (my ssh session) | 1 | active |

All connections came from the internal Docker network
(`172.18.0.0/16`). **No public-IP connections observed.**

**Database stats (`pg_stat_database`):**

```
xact_commit:    1,490,666
xact_rollback:        303
deadlocks:             0
conflicts:             0
temp_files:            0
```

No signs of automated scraping, large data exports, or
schema enumeration.

**PostgreSQL auth log:** empty. No
`authentication failed` or `password` lines. No failed
login attempts recorded.

**Host firewall (`iptables`):** policy `ACCEPT`,
`ufw` inactive — no upstream blocking of port 5432.

**Net result:** the port was exposed but no evidence of
compromise. The alert was a scan finding, not an intrusion
detection.

### Fix applied (commit `0682815`)

In `docker-compose.prod.yml`, removed the
`ports: - "5432:5432"` line from the `postgres:` service
and replaced it with a `SECURITY:` comment block explaining
how the proxy and dashboard still reach PostgreSQL over
the internal Docker network (no port mapping required for
container-to-container resolution).

I also force-recreated the `optitoken-postgres` container
(the named volume `postgres_data` preserves the 401 rows of
telemetry data) so the running container picks up the
new compose without the public port mapping.

**Verify on the prod box:**

```bash
$ docker port optitoken-postgres
# (empty)

$ ss -tlnp | grep ':5432 '
# (empty)

$ docker exec optitoken-postgres psql -c "select count(*) from \"RequestLog\";"
401
```

### What I did NOT change
- No password rotation. The PostgreSQL password is in
  `/root/optitoken/.env`. There was no evidence of credential
  abuse, so I left it alone. If you want a rotation, run:
  1. `openssl rand -base64 32` to generate a new password
  2. Update `POSTGRES_PASSWORD=` in `/root/optitoken/.env`
  3. Restart the stack: `cd /root/optitoken/Optitoken &&
     docker compose -f docker-compose.prod.yml up -d --force-recreate postgres`
  4. The named volume `postgres_data` persists across recreate.
- No firewall change. The container has no listening port on
  the host network now, so a firewall rule would be redundant.
- No TLS change. The DB accepts plaintext on the internal
  network; if you want TLS, set `ssl=on` in
  `postgresql.conf` and mount certs in the container.

### Residual risks to watch
- The Redis container is also bound to `0.0.0.0:6379`. It
  uses a password (`REDIS_PASSWORD`) so it's not open to the
  public, but the same scanner could flag it next. Same
  fix pattern applies: remove the `ports:` block, keep the
  service reachable from the internal network only.
- The proxy container is bound to `0.0.0.0:8080` and Caddy
  is bound to `0.0.0.0:80/443`. This is **intentional** —
  the proxy is the public-facing entry point. Just don't add
  any new container with `0.0.0.0:5432` or any other
  database/credentials port in the future.

### How to verify the fix is still in place

```bash
ssh root@<prod>
cd /root/optitoken/Optitoken
git log --oneline -3
# should show 0682815 fix(security): remove public 5432...
docker port optitoken-postgres
# should be empty
ss -tlnp | grep ':5432 '
# should be empty
```

If a future git pull undoes the change, the last line will
non-empty and the alert will fire again.