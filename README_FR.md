<p align="center">
  <img src="docs/assets/logo.png" alt="Logo Synapse Proxy" width="650" />
</p>

<p align="center">
  <a href="https://synapse-proxy.com"><img src="https://img.shields.io/badge/Website-synapse--proxy.com-indigo.svg?style=flat-square" alt="Website"></a>
  <img src="https://img.shields.io/badge/Status-Active-success.svg?style=flat-square" alt="Status">
  <img src="https://img.shields.io/badge/Licence-MIT-blue.svg?style=flat-square" alt="Licence">
  <img src="https://img.shields.io/badge/Rust-1.75+-orange.svg?style=flat-square" alt="Version Rust">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8.svg?style=flat-square" alt="Version Go">
  <img src="https://img.shields.io/badge/Next.js-14+-black.svg?style=flat-square&logo=next.js" alt="Next.js">
  <img src="https://img.shields.io/badge/API_OpenAI-Compatible-green.svg?style=flat-square&logo=openai" alt="Compatible OpenAI">
</p>

<h1 align="center">Synapse Proxy : Le Pare-feu Agentique & Plateforme d'Observabilité Incontournable</h1>

> **Un proxy open-source transparent qui apporte une observabilité de qualité militaire, une sécurité absolue et un cache intelligent à vos agents IA autonomes.**

Synapse Proxy s'intercale élégamment entre votre application et n'importe quel fournisseur de LLM compatible OpenAI. Sa mission principale est de fournir une **Observabilité Agentique Profonde** et un **Pare-feu Intelligent**, afin de maîtriser les boucles infinies de vos agents, de protéger vos données sensibles, et de rendre les interactions complexes totalement transparentes et mesurables.

<p align="center">
  <img src="docs/assets/dashboard_demo.webp" alt="Démo Animée du Dashboard" width="800" />
</p>

Tout en protégeant activement votre infrastructure et en analysant les intentions de vos agents, Synapse Proxy optimise silencieusement l'utilisation des tokens en arrière-plan grâce à un pipeline de cache à 4 niveaux (L0 à L3), vous assurant de ne jamais payer deux fois pour le même processus de pensée agentique.

**English version**: [README.md](README.md)

---

## 🛡️ Pare-feu Agentique & Sécurité

Lors de la création d'agents autonomes (AutoGPT, LangChain, boucles personnalisées), le risque majeur réside dans les boucles infinies, l'explosion des coûts et les injections de prompt. Synapse Proxy introduit un Pare-feu robuste, conçu spécifiquement pour les agents IA :

- **Loop Kill Switch & Auto-Correction :** Détecte lorsqu'un agent dérive dans une boucle infinie (appels d'outils répétés). Il intercepte l'exécution et renvoie une réponse simulée compatible OpenAI (`HTTP 200`) contenant un avertissement d'auto-correction, guidant l'agent à changer de stratégie sans faire planter le processus.
- **Liste Blanche d'Outils (Tool Allowlist) & Empreintes :** Verrouillez les capacités de votre agent. Si un agent hallucine un outil ou tente d'invoquer une fonction non autorisée, le Proxy bloque activement la requête.
- **Configurations Granulaires des TTL de Cache :** Personnalisez la durée de vie du cache par outil (y compris un TTL de 0s pour désactiver le cache sur certains outils stateful ou sensibles).
- **Rédaction PII (Anonymisation) :** Masquage natif basé sur des expressions régulières pour protéger les données sensibles (E-mails, Numéros de téléphone, Clés API) avant même que le prompt n'atteigne le fournisseur LLM.
- **Disjoncteur par Session (Circuit Breaker) :** Définissez des limites strictes de tokens par session pour plafonner les dépenses sur une tâche donnée.

---

## 📊 Télémétrie Profonde & Observabilité des Intents

Chaque requête est persistée dans une base de données PostgreSQL, transformant le comportement "boîte noire" de vos agents en un flux transparent et analysable via notre impressionnant Dashboard de Contrôle Next.js.

- **Classification d'Intents par IA Locale :** Nous utilisons `@xenova/transformers` (exécuté localement, 100% hors ligne) pour classifier de manière asynchrone l'intention de chaque prompt (`coding`, `rag`, `chat`, `extraction`) sans ajouter une seule milliseconde de latence sur le chemin critique du proxy.
- **Timeline Session Replay :** Inspectez les interactions de l'agent étape par étape. Reconstruisez le flux de l'agent, les appels d'outils et la latence de chaque payload sur une frise chronologique visuelle unifiée.
- **Diff du System Prompt :** Les agents réécrivent parfois leurs propres instructions en cours de session. Le proxy extrait et compare (diff) le prompt système, mettant en évidence ce qui a changé directement dans le dashboard.
- **Tracker de Fenêtre de Contexte :** Un graphique dynamique comparant les *Tokens du Prompt Original* aux *Tokens Compressés L3* au fil du temps, démontrant exactement comment le contexte grandit et comment Synapse Proxy le mitige.
- **Benchmark A/B :** Activez le mode benchmark pour lancer des requêtes de contrôle et optimisées en parallèle, en utilisant un LLM Juge pour évaluer la similarité des réponses.

<p align="center">
  <img src="docs/assets/flow.png" alt="Flux Synapse Proxy" width="650" />
</p>

---

## ⚡ L'Optimisation des Coûts en Bonus

Bien que la sécurité et l'observabilité soient au cœur du système, Synapse Proxy intègre un moteur de cache à la pointe de la technologie conçu pour minimiser la latence et le gaspillage de tokens.

- **Remplacement direct d'OpenAI :** Aucune modification de SDK n'est requise. Pointez simplement votre client vers `http://<host>:8080/v1` avec une clé virtuelle `Authorization: Bearer sk-opti-...`.
- **Quatre niveaux de cache dans un seul binaire :**
  - **L0 In-flight Dedup :** Bloque et déduplique les requêtes concurrentes identiques (idéal pour le fan-out d'agents).
  - **L1 Exact Match :** Correspondance SHA-256 ultra-rapide pour les scripts qui relancent exactement la même requête.
  - **L2 Semantic Match :** Recherche vectorielle basée sur ONNX (MiniLM) pour les requêtes conceptuellement identiques. Désactivé automatiquement sur les conversations multi-tours pour éviter la corruption d'état.
  - **L3 Compression préservant les préfixes :** Élague intelligemment les anciens blocs `<thought>`, tronque les sorties d'outils surdimensionnées et condense l'historique. Il maintient un préfixe identique à l'octet près afin que le cache de prompt natif du fournisseur (Upstream) reste efficace à 99%. Comprend plusieurs modules avancés :
    - **SmartCrusher :** Détecte les grands tableaux JSON homogènes, extrait leur schéma et les compacte sous forme de lignes CSV optimisées. En cas de saturation du contexte, il applique une troncature intelligente (conserve 30% du début et 15% de la fin) avec les métadonnées `_ccr_dropped` pour garantir l'intégrité syntaxique.
    - **DiffCompressor :** Nettoie les grands diffs git en ne conservant que 2 lignes de contexte inchangées autour des hunks. Les diffs de plus de 50 lignes sont déchargés dans l'archive L3 CCR (Compression Content Repository) et remplacés par des clés d'identification courtes `<<ccr:hash>>`.
    - **ASTCodeCompressor :** Analyse le code source (Python, Go, JS/TS) et élague le corps des fonctions/classes de plus de 5 lignes, en injectant des commentaires d'élision valides pour économiser jusqu'à 70% de tokens de prompt.
    - **Optimisation Anthropic KV-Cache :** Injecte automatiquement la directive `"cache_control": {"type": "ephemeral"}` aux endroits stratégiques (system prompt, liste des outils, avant-dernier message utilisateur) pour maximiser le cache de prompt natif d'Anthropic.
  - **Dédoublonnement Sémantique des Outils (Semantic Tool Dedup) :** Intercepte les appels d'outils du LLM et récupère les résultats mis en cache à partir d'appels similaires, court-circuitant ainsi les boucles d'exécution côté client.

<p align="center">
  <img src="docs/assets/diag_en.png" alt="Diagramme Synapse Proxy" width="650" />
</p>

---

## 🔌 Serveur MCP (Model Context Protocol)

Synapse Proxy agit également comme un serveur MCP robuste, exposant **17 outils spécialisés** directement à votre IDE (Cursor, Claude Code, Continue, etc.).

Tous les outils sont entièrement gratuits et utilisables localement ou sur votre propre stack. L'outil `synapse_chat_completions` s'exécute désormais **in-process** (entièrement en mémoire via `httptest.NewRecorder()`), ce qui élimine toute dépendance à un port d'écoute HTTP et permet au serveur MCP de tourner hors-ligne en mode stdio (`--mcp`).

Les nouveaux outils locaux intégrés comprennent :
* **`synapse_inspect_ccr_store`** : Liste toutes les clés et tailles des payloads archivés dans le cache L3 CCR.
* **`synapse_get_ccr_value`** : Récupère la chaîne de caractères originale correspondant à une référence de clé CCR.
* **`synapse_optimize_prompt`** : Simule localement le pipeline de compression (BeforeRequest) et retourne le prompt optimisé et les alertes sans appeler de LLM.

```bash
# Mode stdio (recommandé pour l'intégration locale de Cursor / IDE)
./synapse-proxy --mcp --mcp-tier=full

# Mode HTTP SSE (pour les déploiements serveurs distants ou multi-utilisateurs)
./synapse-proxy --mcp-http --mcp-http-port=8081 --mcp-tier=full --dashboard-url=http://localhost:3000
```

> 📖 **Pour aller plus loin :** Consultez le [Guide du Serveur MCP](docs/mcp_server.md) pour obtenir la liste complète des outils, leurs schémas de paramètres et les instructions de configuration de l'IDE.

---

## 💻 Le Dashboard (Next.js) — 100% Open Source

Le repo inclut un dashboard Next.js complet sous `./dashboard` qui transforme la télémétrie brute du proxy en un véritable centre de commandement. **Il est entièrement open source sous la même licence MIT que le proxy** : auditez-le, forkez-le, auto-hébergez-le, théméz-le — il n'y a pas de voie fermée SaaS-only.

| Fonctionnalité | Description |
|----------------|-------------|
| **Live Telemetry** | Voyez chaque requête arriver via SSE. Les requêtes partageant une conversation sont automatiquement groupées grâce à une signature unique. |
| **Global Command Center** | Une interface 3D/HUD époustouflante affichant les flux de tokens en temps réel, les taux de cache hit, et la santé des serveurs. |
| **Agent Firewall Modal** | Activez par clé : caches L1/L2/L3, kill switch, rédaction PII, plafond de tokens par session, liste blanche d'outils. Les changements se synchronisent dans Redis instantanément. |
| **Playground v3** | Chat A/B côte-à-côte : même prompt lancé en parallèle via le proxy et en direct. Inclut un moteur de rendu d'artifacts interactif. |
| **Admin Panel** | Auto-hébergez tout le produit : gestion des clés virtuelles, pricing dynamique des modèles, gestion utilisateurs, règles d'alerte, et facturation Stripe. |

### Architecture

```
dashboard/
├── app/                        # Next.js App Router (React Server Components)
├── components/                 # LiveTelemetryGrouped, FirewallModal, TokenFlowAnimation, etc.
├── lib/                        # authOptions, prisma, stripe, email
├── prisma/                     # Schéma PostgreSQL & migrations
└── .env.example                # Template de configuration
```

Le dashboard lit sur les mêmes instances Postgres + Redis que le proxy, donc un déploiement auto-hébergé a **une seule base à sauvegarder**.

---

## 🚀 Démarrage Rapide (Getting Started)

### 1. Auto-hébergement via Docker Compose
Clonez le dépôt et montez toute l'architecture (Proxy, Postgres, Redis, Dashboard Next.js, Caddy) en une seule commande :

```bash
git clone https://github.com/yourusername/synapse-proxy.git
cd synapse-proxy

# Copiez les variables d'environnement d'exemple
cp .env.example .env

# Construisez et lancez la stack
docker compose -f docker-compose.prod.yml up -d --build
```

### 2. Quickstart (Code Client)
Une fois votre proxy lancé, modifier votre code est aussi simple que de mettre à jour la `baseURL` et l'`apiKey` :

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1", # Pointez vers Synapse Proxy
    api_key="sk-opti-..."                # Utilisez votre clé virtuelle Synapse
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Bonjour !"}]
)
```

---

## 📄 Licence

Synapse Proxy est **entièrement open source sous Licence MIT** — proxy, dashboard et SDKs compris. Auto-hébergez la stack complète, auditez chaque ligne, forkez ce dont vous avez besoin. 

Nous proposons un SaaS managé sur [synapse-proxy.com](https://synapse-proxy.com) pour les équipes qui préfèrent ne pas opérer Postgres + Redis elles-mêmes ; la version hébergée exécute exactement le même code que ce repo. Le SaaS est une **commodité**, pas un gardien.
