# Synapse Proxy - Client Local Windows (`.exe` autonome)

Ce répertoire contient le client local autonome Windows pour Synapse Proxy, développé en Go avec base SQLite embarquée et dashboard Next.js compilé et intégré via `go:embed`.

---

## 🚀 Fonctionnalités du Client Local

### Endpoints
1. **Port Dashboard Unique : `4321`** (accessible sur `http://localhost:4321`).
2. **Port Proxy : `8080`** (accessible sur `http://localhost:8080/v1`).

### Caches locaux (SQLite `synapse_local.db`)
- **L1** : hash SHA-256 du payload post-L3 → réponse.
- **L2** : similarité Jaccard ≥ 0.85 sur le system prompt.
- **L3** : similarité Jaccard ≥ 0.70 sur le system prompt OU le dernier message user (émule CCR chunk-based).

### Pipeline de compression L3 (byte-preserving)
Le proxy applique automatiquement les règles L3 sur **chaque** requête avant le cache lookup et l'upstream :

| Règle | Effet | Carve-out |
|-------|-------|-----------|
| Troncature tool output | `content > 200 chars` → 200 chars + marker `[…truncated by Synapse L3…]` | Préserve les todo-lists (`status: pending` etc.) |
| Strip thinking blocks | Retire `<thinking>...</thinking>`, `<thought>...</thought>`, `<scratchpad>...</scratchpad>` | Aucun |
| Drop repeated tool results | 3ème+ tool result consécutif du même tool → `content: ""` | Préserve les todo-lists |
| Strip reasoning_content | Retire le champ `reasoning_content` des messages assistant non-récentes | Aucun |

**Byte-stable** : la clé d'API et l'ordre des autres champs du payload sont préservés → le cache L1 (hash exact) reste cohérent entre les requêtes.

### Pipeline Anthropic (optionnel)
Si le provider est `anthropic`, `minimax`, ou `minimax-anthropic`, le proxy :
1. Traduit le payload OpenAI → format Anthropic `/v1/messages`
2. Insère le bon `Authorization: x-api-key` header
3. Forwarde vers l'endpoint Anthropic-compatible du provider
4. Traduit la réponse Anthropic → OpenAI pour le client

**Bénéfice** : active le prompt cache du provider (cache_read à 0.1× tarif input sur la version Anthropic, ou cache automatique Minimax).

### Intégration Ollama & LM Studio
- **Ollama** : par défaut sur `http://localhost:11434`.
- **LM Studio** : par défaut sur `http://localhost:1234`.

Les modèles sont détectés dynamiquement et listés dans le Playground.

### DRM de Quotas Hors Ligne
Limites locales (FREE 10M, PRO 50M, ENTERPRISE illimité) synchronisées toutes les 10 minutes avec l'API Cloud Synapse.

---

## 🛠️ Guide de Compilation et Build

### Étape 1 : Générer le Dashboard Statique Next.js
1. Allez dans le répertoire du dashboard principal :
   ```bash
   cd dashboard
   ```
2. Installez les dépendances :
   ```bash
   npm install
   ```
3. Configurez Next.js pour l'export statique en vous assurant que `next.config.js` contient `output: 'export'`.
4. Lancez la compilation :
   ```bash
   npm run build
   ```
5. Copiez le contenu de `out/` vers le sous-dossier d'embed du client local :
   ```bash
   xcopy /E /I /Y out ..\local-client\internal\dashboard\dashboard-static
   ```

Ou plus simple, utilisez le script PowerShell à la racine :
```powershell
.\build-static-dashboard.ps1
```

### Étape 2 : Compiler le Binaire Go (.exe)
1. Allez dans le répertoire du client local :
   ```bash
   cd local-client
   ```
2. Lancez la compilation du binaire Windows :
   ```bash
   go build -o synapse-local.exe .
   ```
3. Optionnel : lancez les tests unitaires :
   ```bash
   go test ./internal/compress/
   ```

---

## 🤖 Guide de Configuration des Outils IA (Agents)

Pour connecter vos outils de développement au proxy de compression Synapse et commencer à économiser jusqu'à 85% de tokens, suivez ces instructions :

### 1. Claude Code
Configurez les variables d'environnement dans votre terminal avant de lancer Claude Code :
```bash
# Pointer vers le proxy local Synapse
export CLAUDE_BASE_URL="http://localhost:8080/v1"
# Fournir votre clé virtuelle générée (ex: sk-opti-...)
export ANTHROPIC_API_KEY="sk-opti-votre-cle-ici"
# Lancer Claude normalement
claude
```

### 2. Cursor
1. Allez dans les **Settings** (icône d'engrenage en haut à droite).
2. Cliquez sur **Models** puis ouvrez la section **OpenAI** ou **Anthropic**.
3. Cochez/Activez le provider de votre choix.
4. Entrez votre clé virtuelle (ex: `sk-opti-...`) dans le champ de clé API.
5. Cliquez sur **Override URL** et configurez l'URL suivante :
   `http://localhost:8080/v1`
6. Enregistrez. Vos requêtes Cursor passeront désormais par Synapse Proxy !

### 3. Claude Desktop
Modifiez votre fichier de configuration `claude_desktop_config.json` :
```json
{
  "mcpServers": {},
  "api": {
    "url": "http://localhost:8080/v1",
    "key": "sk-opti-votre-cle-ici"
  }
}
```

---

## 📦 Distribution

Le zip `synapse-local-windows.zip` contient :
- `synapse-local.exe` (binaire standalone)
- `README.md` (ce document, français, complet)

Le zip `synapse-local-windows.zip` (l'ancien format) contenait :
- `synapse-local.exe`
- `README.txt` (quick start anglais, pour distribution internationale)

Pour la distribution publique, utilisez le zip en anglais :

```bash
Compress-Archive -Path "synapse-local.exe", "README.txt" -DestinationPath "synapse-local-windows.zip"
```

---

## 🔧 Architecture interne

```
local-client/
├── main.go                       # entrée du binaire
├── resource.rc / resource.syso  # icône Windows
├── build-static-dashboard.ps1    # compile le dashboard Next.js
├── internal/
│   ├── proxy/                    # HandleChatCompletions
│   │                             # pipeline: L3 compress → cache → upstream
│   ├── compress/                 # Compress + CompressBytePreserving
│   │                             # + OpenAIToAnthropic + AnthropicToOpenAI
│   │                             # + tests
│   ├── cache/                    # L1/L2/L3 avec PayloadContext
│   ├── db/                       # SQLite init
│   ├── license/                  # DRM de quotas
│   └── dashboard/                # UI Next.js embarquée via go:embed
└── README.md, README.txt        # documentation
```

### Pipeline `HandleChatCompletions` (résumé)

```
incoming payload
    ↓
1. compress.RunBefore(payload, provider, defaultModel)
    ├─ CompressBytePreserving(payload)
    │   ├─ tool output > 200 chars → tronqué
    │   ├─ <thinking>...</thinking> → stripé
    │   └─ 3ème+ tool result même nom → blank
    └─ OpenAIToAnthropic (si provider anthropic-compatible)
    ↓
2. cache.MakePayloadContext(payload)
    → SHA-256 + signature (system head, last user, tools hash)
    ↓
3. cache.GetL1 / GetL2 / GetL3(pc)
    → si hit : réponse cached, X-Synapse-Cache: HIT
    ↓
4. upstream POST
    ├─ Ollama (localhost:11434)
    ├─ LM Studio (localhost:1234)
    ├─ Anthropic (api.anthropic.com/v1/messages)
    ├─ Minimax Anthropic (api.minimax.io/anthropic/v1/messages)
    └─ OpenAI (api.openai.com/v1/chat/completions)
    ↓
5. compress.RunAfter(response, provider, defaultModel)
    └─ AnthropicToOpenAI (si provider anthropic-compatible)
    ↓
6. cache.SetL1(pc, response) si 200
    ↓
response to client
```

### Quand le cache hit arrive
- **L1** : payload byte-identique entre deux requêtes successives (même system, même user, même tools, même structure).
- **L2** : system prompt à 85% Jaccard avec une entrée cachée. Convient aux conversations où seul le system prompt est stable.
- **L3** : system prompt OU dernier user message à 70% Jaccard. Plus permissif, accepte les reformulations.

### Quand le cache miss arrive
- Premier appel à un nouveau system prompt
- Outils différents entre les requêtes (ToolsHash différent)
- Nouveau tool name jamais vu

---

## 🎛️ Providers Locaux : Ollama & LM Studio

Synapse Proxy local détecte et intègre vos LLM s'exécutant sur votre machine.

* **Ollama** : Écoute par défaut sur `http://localhost:11434`.
* **LM Studio** : Écoute par défaut sur `http://localhost:1234`.

Lorsque vous créez ou configurez une clé virtuelle avec Ollama ou LM Studio comme provider principal, le dashboard interroge dynamiquement les instances locales pour lister tous les modèles de langage installés (`llama3`, `mistral`, `deepseek-coder`, etc.) directement dans le Playground local !