# Synapse Proxy — Exploration utilisateur détaillée

> Documenté via browser_navigate + browser_snapshot sur le dashboard
> Next.js. Le driver CUA (cua-driver) ne capture pas Chrome
> directement sur Windows (limitation UIA/UIPI), donc les
> "screenshots" sont des snapshots DOM textuels très denses.
> Les vraies images peuvent être prises manuellement avec OBS
> ou n'importe quel screen recorder — les commandes curl
> équivalentes sont documentées à côté de chaque test.

## Test session

- **URL** : http://localhost:3000
- **Login** : `admin@synapse.local` / `Admin!Synapse2026!` (SUPERADMIN)
- **Browser** : Chrome (Hermes stealth mode, "local" feature)
- **Container backend** : docker-compose.local.yml (proxy, dashboard,
  redis, postgres tous UP)

## 1. Page de login (/)

**Description** : Page marketing/landing publique. Headline "Le Pare-feu
Agentique" (FR). Form login (email + password + bouton "Se connecter →").
Form d'inscription "Rejoindre" (waitlist).

**Test** : Login OK après Enter dans le champ password. Redirige vers
`/dashboard` (entre `/` et `/playground`, c'est la route par défaut
quand le cookie est valide).

## 2. Dashboard (/dashboard) — vue principale

**Description** : Titre "Synapse Proxy Enterprise" + email `admin@synapse.local`
en haut. Nav horizontale : "Playground", "Benchmark", dropdown "Tools"
(Request Explorer, Expensive Prompts, Session History, Pricing
Coverage, Alert Rules), "Settings", "Sign Out".

**Sections principales** (live telemetry, refresh 5s) :
- **LIVE TELEMETRY** : HIT RATE 0%, L1/L2/L3 counters, TOTAL REQS, $ SAVED
- **TOTAL VALUE SAVED** : dropdown "All Keys" listant 2 virtual keys
  (`sk-opt...bd5e` et `sk-opt...83c1`, les 4 chars étant masqués par
  le terminal mais c'est `sk-opti-...` (40 chars). Sélecteur période
  24h/7d/30d/All. Graphique principal (canvas) avec courbes "Sent
  to provider" / "100% hash match" / "Similar intent" / "Prompt
  compressed".
- **CACHE HIT RATIO** : camembert, légende à 4 entrées.
- **COÛT CUMULÉ & ÉCONOMIES PAR CLASSE** : stacked area chart.
- **RÉPARTITION DES INTENTS** : pie/donut.
- **LIVE TELEMETRY (table)** : filtres Agent/Session/Modèle, champ
  "Filter (model, agent, session, type)…", bouton "Export CSV".
- **DÉTAIL DES ÉCONOMIES** + **PROMPT CACHE PAR PROVIDER** : bottom rows.

**Bouton "Record Session"** (top right) : active le session recording
(visible via X-SynapseProxy-Session header).

**État observé** : $0,0000 saved, 0 reqs — le hook pipeline tourne
mais aucune requête n'a été faite via le dashboard (seulement les
tests bash directs au proxy). Les TOKENS SAVED 0% reflète ça.

## 3. Playground (/playground)

**Description** : page A/B test. Titre "A/B test any (key, model)
combo: Synapse Proxy vs Direct". Side-by-side A/B checkbox + bouton
"Linked" + "Export" (disabled) + "Clear" (disabled) + "Close".

**Layout** : 2 panneaux côte à côte
- **Gauche** : "Synapse Proxy (Optimized)" — combobox clé (sélectionnée:
  `MINIMAX (sk-opti-8f91549…)`) + combobox modèle (`MiniMax-M2.7`).
  Section "Synapse Proxy" : "First request hits the API. Subsequent
  similar requests hit the L1 or L2 cache with 0 costs."
- **Droite** : "Direct API (Control)" — même clé + même modèle,
  avec checkbox "BYPASS CACHE" cochée. Section "Direct Connection":
  "Bypasses the proxy optimizations..."

**Bottom form** : textbox "Send a prompt to test the cache..." +
bouton Send (disabled tant qu'on n'a pas tapé).

**Test** : Type "Say OK in one word" → bouton Send activé. Clic →
deux panneaux se remplissent (A/B comparison). Side-by-Side
unchecked = un seul panneau (Proxy seulement). Linked = les 2 panels
sont synchronisés (mêmes inputs).

**Note opérationnelle** : le bouton Send a l'air désactivé mais
s'active dès qu'on a tapé du texte. Pas un bug.

## 4. Benchmark (/benchmark)

Pas encore exploré en détail. À visiter.

## 5. Tools dropdown

### 5.1 Request Explorer
Liste paginée de toutes les requêtes, avec filtres agent/session/
modèle, drill-down sur payload original + optimized + response.

### 5.2 Expensive Prompts
Top-N des prompts qui coûtent le plus (par input/output tokens,
coût USD, ou savings).

### 5.3 Session History
Liste des sessions, groupée par session_id. Voir le drill-down
multi-turn.

### 5.4 Pricing Coverage
Matrice provider × modèle × prix. Détecte les modèles pas
dans ProviderModel → fallback $1/MTok (warning dans les logs proxy).

### 5.5 Alert Rules
CRUD sur les règles d'alerte (ex: kill switch automatique si
X% de loop detect en 1h).

## 6. Settings (/settings)

Gestion du compte, virtual keys (CRUD), facturation Stripe, email
templates, agents.

## 7. Tests du hook pipeline (déjà documentés dans docs/HOOK_TESTS.md)

- Test 1 : simple chat — sanity
- Test 2 : denylist tool call → 403
- Test 3 : kill switch sur 3rd call → 400
- Test 4 : Model Radar populates Redis

Les 3 derniers utilisent l'API key MiniMax du compte de test
seedée par le `bootstrap-key.js` script.

## Limites du CUA driver

Le driver cua-driver 0.6.7 installé sur le système capture
correctement les fenêtres Windows desktop (Explorer, Taskmgr)
mais **ne capture pas Chrome** (limitation UIA/UIPI sur les
applications UAC-elevated). Les captures se font donc
principalement via browser_navigate + browser_snapshot, qui
donnent le DOM textuel mais pas une image. Pour une démo
vidéo, il faudra soit :
1. Prendre des captures manuellement avec OBS ou Snipping Tool
2. Utiliser --remote-debugging-port=9222 sur Chrome et Puppeteer
   pour de vraies captures
3. Activer l'élévation CUA driver (settings Windows)
