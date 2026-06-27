# SECURITY.md — Synapse Proxy

> Document de sécurité pour la mise en production. La dernière
> révision est datée du 2026-06-24 suite à l'incident Redis
> 167.233.60.226.

## Incident 2026-06-23 — Redis exposed

**Système affecté** : `167.233.60.226` (ASN 24940)
**Version Redis** : 7.4.7
**Détecté le** : 2026-06-23 03:21:03 UTC
**Statut** : non corrigé au moment de la rédaction.

### Symptôme
- Redis 7.4.7 répond sur le port 6379 depuis Internet
- Aucune authentification SASL configurée
- N'importe qui peut lire, écrire, supprimer toutes les données
- Risque : vol de credentials (clés API chiffrées, JWT NextAuth),
  suppression de données, injection de requêtes frauduleuses

### Cause
- Le serveur Redis écoute sur `0.0.0.0:6379` au lieu de `127.0.0.1:6379`
- Pas de `requirepass` dans `redis.conf`
- Pas de pare-feu (firewall Windows / iptables) qui bloque le port 6379

### Actions correctives
1. **Immédiat (à faire dans l'heure)** : couper le service Redis
   exposé OU ajouter une règle de firewall bloquant le port 6379
   depuis l'extérieur.
2. **Court terme (24h)** : ajouter `requirepass <strong-password>`
   dans `redis.conf` et redémarrer Redis.
3. **Moyen terme (semaine)** : modifier `bind 127.0.0.1` dans
   `redis.conf` (ou `bind 0.0.0.0` + `protected-mode yes` + un
   reverse-proxy avec TLS en frontal).
4. **Moyen terme** : activer `rename-command CONFIG ""` et
   `rename-command FLUSHDB ""` pour limiter les primitives
   dangereuses.
5. **Long terme** : monitoring continu (alerte si quelqu'un tente
   un `INFO` ou un `CONFIG SET` non autorisé).

### Configuration recommandée (redis.conf)

```conf
# Bind to loopback only by default
bind 127.0.0.1 ::1

# Require authentication
requirepass <strong-password-generated-with-openssl-rand-base64-32>

# Protected mode (default since Redis 3.2, but make it explicit)
protected-mode yes

# Disable dangerous commands
rename-command CONFIG ""
rename-command FLUSHDB ""
rename-command FLUSHALL ""
rename-command SHUTDOWN ""

# Logging (helps detect brute-force)
loglevel notice
logfile /var/log/redis/redis-server.log
```

### Vérification post-fix

```bash
# Depuis l'extérieur, doit échouer (timeout ou connection refused)
redis-cli -h 167.233.60.226 -p 6379 PING

# Depuis localhost, doit fonctionner
redis-cli -h 127.0.0.1 -p 6379 -a "$REDIS_PASSWORD" PING
```

## Sécurité du proxy en production

### Authentification
- Le proxy vérifie un Bearer token `sk-opti-...` (32 chars hex) sur
  chaque requête entrante (interceptor dans `internal/handlers/proxy.go`).
- L'absence de token ou un token invalide retourne 401.
- Les tokens sont générés aléatoirement par
  `internal/services/crypto.go` (crypto/rand).

### Chiffrement des clés API en base
- Les clés API des providers upstream (OpenAI, Anthropic, etc.)
  sont chiffrées avec AES-256-GCM (`internal/services/crypto.go`).
- La clé de chiffrement est dans la variable d'environnement
  `ENCRYPTION_KEY` (32 bytes).
- En cas de leak de la DB Postgres, les clés upstream ne sont pas
  lisibles directement.

### Rate limiting
- Le hook `SessionCBHook` (optiagent) implémente un rate limit
  par virtual key.
- Le hook `LoopDetectionHook` détecte les boucles infinies et
  coupe la requête (kill switch).
- Le hook `ToolFilterHook` applique un denylist/allowlist par
  virtual key.

### Audit logs
- Toutes les requêtes sont loggées dans `RequestLog` (Prisma)
  avec : timestamp, model, agent, tokens, latence, $ saved.
- Les alertes (Alert Rules) permettent de détecter les anomalies
  (ex: kill switch si loop_count > 5 en 60s).

## Sécurité du dashboard

### Authentification
- NextAuth (CredentialsProvider) avec bcrypt pour les passwords.
- Cookie HttpOnly + Secure (en prod).
- CSRF token sur les formulaires sensibles.

### Isolation
- Le dashboard est dans un container Docker séparé du proxy.
- Communication via `http://synapse-proxy:8080` (interne Docker).
- Pas d'accès direct au proxy depuis l'extérieur.

## Variables d'environnement sensibles

| Variable | Stockage | Usage |
|----------|----------|-------|
| `ENCRYPTION_KEY` | `.env` (gitignored) | Chiffrement clés API |
| `NEXTAUTH_SECRET` | `.env` (gitignored) | Signature JWT NextAuth |
| `DATABASE_URL` | `.env` (gitignored) | Postgres connection |
| `REDIS_URL` | compose (no secret) | Redis interne Docker |

⚠️ **Ne JAMAIS commit un `.env` réel** dans le repo. Le `.env`
  de prod est sur le serveur de prod uniquement.
