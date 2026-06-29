# Synapse Proxy - Client Local Windows (`.exe` autonome)

Ce répertoire contient le client local autonome Windows pour Synapse Proxy, développé en Go avec base SQLite embarquée et dashboard Next.js compilé et intégré via `go:embed`.

---

## 🚀 Fonctionnalités du Client Local
1. **Port Dashboard Unique : `4321`** (accessible sur `http://localhost:4321`).
2. **Port Proxy : `8080`** (accessible sur `http://localhost:8080/v1`).
3. **Caches locaux L1 / L2 (Sémantique Jaccard) / L3 (CCR)** persistés dans une base SQLite légère (`synapse_local.db`).
4. **Intégration locale Ollama & LM Studio** : Détection automatique des modèles locaux et auto-complétion dans le Playground.
5. **DRM de Quotas Hors Ligne** : Limites locales (FREE 10M, PRO 50M, ENTERPRISE illimité) synchronisées toutes les 10 minutes avec l'API Cloud Synapse.

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
   *Ceci génère un dossier `out/` contenant tous les fichiers HTML/JS/CSS statiques.*
5. Copiez le contenu de `out/` vers le sous-dossier d'embed du client local :
   ```bash
   xcopy /E /I /Y out ..\local-client\internal\dashboard\dashboard-static
   ```

### Étape 2 : Compiler le Binaire Go (.exe)
1. Allez dans le répertoire du client local :
   ```bash
   cd local-client
   ```
2. Lancez la compilation du binaire Windows :
   ```bash
   go build -o synapse-local.exe main.go
   ```
   *Vous obtiendrez un exécutable autonome `synapse-local.exe` pesant environ 20-30 Mo.*

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

## 🎛️ Providers Locaux : Ollama & LM Studio

Synapse Proxy local détecte et intègre vos LLM s'exécutant sur votre machine.

* **Ollama** : Écoute par défaut sur `http://localhost:11434`.
* **LM Studio** : Écoute par défaut sur `http://localhost:1234`.

Lorsque vous créez ou configurez une clé virtuelle avec Ollama ou LM Studio comme provider principal, le dashboard interroge dynamiquement les instances locales pour lister tous les modèles de langage installés (`llama3`, `mistral`, `deepseek-coder`, etc.) directement dans le Playground local !
