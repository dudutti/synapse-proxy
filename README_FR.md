<p align="center">
  <img src="docs/assets/logo.png" alt="Logo Synapse Proxy" width="650" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Status-Active-success.svg" alt="Status">
  <img src="https://img.shields.io/badge/Licence-MIT-blue.svg" alt="Licence">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8.svg" alt="Version Go">
  <img src="https://img.shields.io/badge/API%20OpenAI-Compatible-orange.svg" alt="Compatible OpenAI">
</p>

<h1 align="center">Synapse Proxy : Le Pare-feu Agentique & Passerelle d'Observabilité</h1>

> **Un proxy open-source transparent qui apporte observabilité, sécurité et optimisation intelligente à vos agents IA autonomes.**

Synapse Proxy s'intercale élégamment entre votre application et n'importe quel fournisseur de LLM compatible OpenAI. Sa mission principale est de fournir une **Observabilité Agentique** sans précédent et un **Pare-feu Intelligent**, afin de maîtriser les boucles infinies de vos agents, de protéger vos données sensibles (PII), et de rendre les interactions complexes totalement transparentes et mesurables.

Tout en protégeant activement votre infrastructure, Synapse Proxy optimise silencieusement l'utilisation des tokens en arrière-plan grâce à un pipeline de cache à 4 niveaux (L0 à L3), vous assurant de ne jamais payer deux fois pour le même raisonnement agentique.

**English version**: [README.md](README.md)

---

## 🛡️ Pare-feu Agentique & Sécurité

Lors de la création d'agents autonomes, le risque majeur réside dans les boucles infinies et l'explosion des coûts. Synapse Proxy introduit un Pare-feu (Firewall) robuste, conçu spécifiquement pour les agents IA :

- **Loop Kill Switch & Auto-Correction :** Détecte lorsqu'un agent dérive dans une boucle infinie (requêtes identiques répétées). Il intercepte l'exécution et renvoie une réponse simulée compatible avec OpenAI (`HTTP 200`) contenant un avertissement d'auto-correction pour guider l'agent à changer de stratégie.
- **Configurations Granulaires des TTL de Cache :** Personnalisez la durée de vie du cache par outil (y compris un TTL de 0s pour désactiver le cache pour certains outils stateful) depuis le tableau de bord du Pare-feu.
- **Rédaction PII (Anonymisation) :** Masquage natif basé sur des expressions régulières pour protéger les données sensibles (E-mails, Clés API) avant même que le prompt n'atteigne le fournisseur LLM.
- **Liste Blanche d'Outils (Tool Allowlist) :** Verrouillez les capacités de votre agent. Si un agent hallucine un outil ou tente d'invoquer une fonction non autorisée, le Proxy bloque activement la requête.
- **Disjoncteur par Session (Circuit Breaker) :** Définissez des limites strictes de tokens par session pour plafonner les dépenses sur une tâche donnée.

---

## 📊 Observabilité Avancée & Session Replay

Chaque requête est persistée dans une base de données PostgreSQL, transformant le comportement "boîte noire" de vos agents en un flux transparent et analysable.

- **Timeline Session Replay :** Inspectez les interactions de l'agent étape par étape. Reconstruisez le flux de l'agent, les appels d'outils (Tool Calls) et la latence de chaque requête sur une frise chronologique unifiée.
- **Tracker de Fenêtre de Contexte :** Un graphique visuel comparant les *Tokens du Prompt Original* aux *Tokens Compressés L3* au fil du temps, démontrant exactement comment le contexte s'allonge et comment Synapse Proxy le réduit.
- **Diff du System Prompt :** Les agents réécrivent parfois leurs propres instructions en cours de session. Le proxy extrait et compare (diff) le prompt système, mettant en évidence les moindres changements.
- **Export de Dataset (JSONL) :** Exportation en un clic de la trajectoire complète d'une session vers un dataset JSONL, prêt pour le Fine-Tuning.
- **Benchmark A/B :** Activez le mode benchmark pour lancer des requêtes de contrôle et optimisées en parallèle, en utilisant un LLM Juge pour évaluer la similarité des réponses.

> 📖 **Pour aller plus loin :** Vous voulez savoir exactement ce qui est enregistré ? Consultez la documentation sur le [Schéma de Base de Données & Télémétrie](docs/telemetry_schema_fr.md).

<p align="center">
  <img src="docs/assets/flow.png" alt="Flux Synapse Proxy" width="650" />
</p>

---

## ⚡ Cache Intelligent & Optimisation

Bien que la sécurité et l'observabilité soient au cœur du système, Synapse Proxy intègre un moteur de cache à la pointe de la technologie conçu pour minimiser la latence et le gaspillage de tokens.

- **Remplacement direct d'OpenAI :** Aucune modification de SDK n'est requise. Pointez simplement votre client vers `http://<host>:8080/v1` avec une clé virtuelle `Authorization: Bearer sk-opti-...`.
- **Quatre niveaux de cache dans un seul binaire :**
  - **L0 In-flight Dedup :** Bloque et déduplique les requêtes concurrentes identiques (idéal pour le fan-out d'agents).
  - **L1 Exact Match :** Correspondance SHA-256 ultra-rapide pour les scripts qui relancent exactement la même requête.
  - **L2 Semantic Match :** Recherche vectorielle basée sur ONNX (MiniLM) pour les requêtes conceptuellement identiques. Désactivé automatiquement sur les conversations multi-tours pour éviter la corruption d'état.
  - **L3 Compression préservant les préfixes :** Élague intelligemment les anciens blocs `<thought>`, tronque les sorties d'outils surdimensionnées et condense l'historique. Il maintient un préfixe identique à l'octet près afin que le cache de prompt natif du fournisseur (Upstream) reste efficace à 99%.
  - **Dédoublonnement Sémantique des Outils (Semantic Tool Dedup) :** Intercepte les appels d'outils du LLM et récupère les résultats mis en cache à partir d'appels similaires (recherche exacte + vectorielle ONNX avec similarité >90% via Redis VSS), effectuant des appels récursifs upstream pour éviter l'exécution côté client.

> 📖 **Pour aller plus loin :** Découvrez la magie derrière notre cache L3 préservant les préfixes et la recherche sémantique L2 ONNX dans la documentation d'[Architecture de Cache](docs/caching_architecture_fr.md).

<p align="center">
  <img src="docs/assets/diag_en.png" alt="Diagramme Synapse Proxy" width="650" />
</p>

---

## 🔌 Serveur MCP (Model Context Protocol)

Synapse Proxy agit également comme un serveur MCP robuste, s'intégrant parfaitement avec des IDE tels que Cursor, Claude Code et Continue.

Exécutez-le en **mode stdio** pour une utilisation CLI unique, ou en **mode HTTP Streamable** pour un processus longue durée derrière votre reverse proxy.

```bash
# Serveur MCP, mode stdio (un processus par client)
./synapse-proxy --mcp --mcp-tier=free

# Serveur MCP, mode HTTP (Production, SaaS-ready)
./synapse-proxy --mcp-http --mcp-http-port=8081 --mcp-tier=full --dashboard-url=https://synapse-proxy.com
```

**Configuration sans Docker (Exemple Cursor) :**
```jsonc
{
  "mcpServers": {
    "synapse-proxy": {
      "url": "https://synapse-proxy.com/mcp",
      "headers": {
        "Authorization": "Bearer sk-opti-VOTRE_CLE"
      }
    }
  }
}
```

---

## 📊 Dashboard (Next.js) — Entièrement Open Source

Le repo inclut un dashboard Next.js complet sous `./dashboard` qui transforme la télémétrie brute du proxy en panneau de contrôle exploitable. Il est **entièrement open source sous la même licence MIT** que le proxy : auditez-le, forkez-le, auto-hébergez-le, théméz-le — il n'y a pas de voie fermée SaaS-only.

| Capacité | Où ça vit | Pourquoi c'est important |
|---|---|---|
| **Live Telemetry** (groupé par Agent / Session / Model) | `app/page.tsx`, `components/LiveTelemetryGrouped.tsx` | Voyez chaque requête arriver via SSE. Les rows qui partagent une conversation (même system prompt + même tool set) sont auto-groupées via `convSignature` (voir "Détection multiturn" ci-dessous). |
| **Modal Agent Firewall** (par clé) | `components/FirewallModal.tsx` | **La fonctionnalité phare.** Activez par clé : caches L1/L2/L3, kill switch, rédaction PII, plafond de tokens par session, allow-list d'outils. Les changements sont propagés à Redis en temps réel. |
| **Tool-call fingerprinting** (`optiagent/tool_fingerprint.go` + `transport_http.go`) | Côté proxy | Détecte un agent qui appelle le même outil avec les mêmes arguments 4× en 30 s. Renvoie **HTTP 429 + Retry-After: 60** ("Recursive Loop Detected") lorsque le cache miss, donc l'agent fait un backoff au lieu de mourir. |
| **Détection multiturn** (`utils/multiturn.go` + `RequestLog.turnCount`/`convSignature`) | Proxy + DB | Chaque row enregistre `(turnCount, convSignature)` pour que le dashboard groupe les requêtes par conversation naturelle même quand l'agent n'envoie pas de `X-Session-Id`. La signature de conversation est `sha1(system_prompt || tool_names)[:8]`. |
| **Session Summary** (3 graphs d'observabilité) | `app/page.tsx`, `app/admin/sessions/page.tsx` | Context Window (Original vs L3 Compressed), System Prompt Diff (avec `react-diff-viewer-continued`), Agent Flow Timeline (étape par étape avec tool calls). Disponible pour chaque groupe avec 2+ rows. |
| **Playground v3** (`/playground`) | `app/playground/` | Chat A/B côte-à-côte : même prompt 2 fois en parallèle, une fois via le proxy, une fois directement upstream (forcé `X-Bypass-Cache: true`). Badges cache par bulle, sparklines, renderer d'artifacts (`<iframe sandbox>` pour HTML, download pour python/js/etc.). |
| **Request Explorer** (`/admin/explorer`) | `app/admin/explorer/page.tsx` | Table triable et filtrable sur `RequestLog` avec drill-down vers le payload complet + payload optimisé + system prompt. |
| **Admin / Sessions / Pricing / Users** | `app/admin/*` | Auto-hébergez tout le produit : virtual keys, pricing des modèles, alert rules, email campaigns, Stripe billing (mettez `STRIPE_*` en env pour activer). |

### Quoi de neuf (post-lancement)

- **Agent Firewall en tant que concept de premier niveau** — chaque clé virtuelle intègre des configurations complètes de Pare-feu (activation du cache L1/L2/L3, kill switch, limites de tokens, liste blanche d'outils, anonymisation PII, détection de boucle par empreinte et TTL personnalisés par outil). Configurable depuis le Dashboard et synchronisé à Redis.
- **Avertissements d'auto-correction de boucle** — remplace les blocages durs traditionnels (HTTP 400/429) par des messages de complétion HTTP 200 simulant un assistant, avertissant l'agent de l'action répétée pour qu'il s'auto-corrige dynamiquement.
- **Dédoublonnement Sémantique d'Outils** — interception des appels d'outils dans les réponses LLM et résolution via le cache en utilisant des embeddings ONNX et Redis VSS, déclenchant des appels récursifs en amont pour court-circuiter l'exécution côté client.
- **Détection multiturn** — le dashboard regroupe les requêtes par empreinte de conversation au lieu de par row, donc une session de debug 4 tours apparaît comme 1 row avec badges "Tour 1/2/3/4" au lieu de 4 rows séparées.
- **MCP server en mode HTTP** — tourne en processus persistant derrière le même reverse proxy Caddy, expose 14 tools (4 gratuits + 10 payants) à tout IDE compatible MCP.

### Architecture du dashboard

```
dashboard/
├── app/                        # Next.js App Router
│   ├── (auth)/                 # login / signup / forgot-password / reset-password
│   ├── admin/                  # pages admin (sessions / explorer / pricing / users / alerts)
│   ├── api/                    # endpoints REST + SSE (analytics, keys, sessions, telemetry)
│   ├── playground/             # A/B Playground v3
│   └── settings/               # Firewall + Zero-Log + benchmark toggles par clé
├── components/                 # LiveTelemetryGrouped, FirewallModal, RequestExplorer, etc.
├── lib/                        # authOptions, prisma, stripe, email
├── prisma/
│   ├── schema.prisma           # Champs firewall ApiKey, turnCount/convSignature RequestLog
│   └── migrations/             # 2026_06_*: agent_detector, pricing, zero_log, alert_rules, payload_hash, response_payload, multiturn
├── public/                     # logo, diag_en, diag_fr, flow, mega_flow
└── .env.example                # template; .env est git-ignoré
```

Le dashboard lit sur les mêmes instances Postgres + Redis que le proxy, donc un déploiement auto-hébergé a **une seule base à sauvegarder**.

---

## 🛠️ Points de terminaison (Endpoints) API

| Méthode | Chemin | Rôle |
|---------|--------|------|
| `POST`  | `/v1/chat/completions` | Le point d'entrée principal du proxy, compatible OpenAI. |
| `GET`   | `/healthz` | Sonde de vie (Liveness). |
| `GET`   | `/readyz`  | Sonde de disponibilité (Ready). Vérifie les connexions Postgres et Redis. |
| `GET`   | `/metrics` | Métriques Prometheus (cache hits, panics, loop blocks). |
| `GET`   | `/v1/models` | Liste des modèles supportés. |

---

## 📄 Licence

Synapse Proxy est **entièrement open source sous Licence MIT** — proxy, dashboard et SDKs compris. Auto-hébergez la stack complète, auditez chaque ligne, forkez ce dont vous avez besoin. Nous proposons un SaaS managé sur [synapse-proxy.com](https://synapse-proxy.com) pour les équipes qui préfèrent ne pas opérer Postgres + Redis elles-mêmes ; la version hébergée est exactement le même code que ce repo, juste pré-configuré. Le SaaS est une **commodité**, pas un gardien.
