# Synapse Proxy - Guide de Soumission et de Publication sur les Places de Marché MCP

Ce guide détaille les places de marché Model Context Protocol (MCP) disponibles, les procédures étape par étape pour y inscrire **Synapse Proxy**, ainsi que les descriptions textuelles et blocs de configuration techniques prêts à être copiés-collés.

> [!NOTE]
> **Statut de l'Embedder ONNX en Rust** :
> Nous n'avons pas encore implémenté le wrapper CGO/Rust Hugging Face pour l'embedder local. Cette option a été proposée dans le plan technique d'architecture pour remplacer l'embedder Python actuel. L'effort s'est concentré en priorité sur le Rejeu Sandbox et la correction des problèmes d'encodage du Dashboard. Actuellement, Synapse Proxy fonctionne avec l'embedder local basé sur le conteneur Python `synapse-proxy-onnx` (FastAPI + ONNX Runtime).

---

## 🗺️ Les Places de Marché et Procédures de Soumission

### 1. Smithery.ai (Le plus populaire pour l'intégration automatique)
Smithery est une place de marché incontournable pour l'installation d'outils MCP en un clic.
* **Procédure** :
  1. Rendez-vous sur [smithery.ai/new](https://smithery.ai/new).
  2. Connectez-vous avec votre compte GitHub.
  3. Renseignez l'URL de votre dépôt public GitHub : `https://github.com/dudutti/synapse-proxy`.
  4. L'outil de Smithery va automatiquement scanner votre projet à la recherche de fichiers de configuration (comme un `package.json` ou un `server.json` si applicable) pour essayer de générer la configuration d'installation automatique.
* **Optimisation par Métadonnées (Recommandé)** :
  Pour garantir un scan parfait sans faille, vous pouvez ajouter un fichier `smithery.yaml` ou un fichier de configuration `.well-known/mcp/server-card.json` à la racine de votre projet.

### 2. Le Registre Officiel MCP (modelcontextprotocol.io)
Géré par le comité de pilotage du protocole MCP (Anthropic/drapeau officiel).
* **Procédure** :
  1. Créez un fichier `server.json` à la racine du projet qui décrit le paquetage de déploiement de votre application (par exemple, s'il y a un tag npm ou Docker).
  2. Installez le package CLI officiel : `npm install -g mcp-publisher`.
  3. Authentifiez-vous auprès du registre officiel à l'aide de votre compte GitHub.
  4. Lancez la publication : `mcp-publisher publish`.

### 3. Awesome MCP Servers (Liste Curatée sur GitHub)
La liste de référence communautaire la plus lue par les développeurs.
* **Procédure** :
  1. Forkez le dépôt [github.com/modelcontextprotocol/awesome-mcp](https://github.com/modelcontextprotocol/awesome-mcp) (ou une variante populaire comme [github.com/punkpeye/awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers)).
  2. Modifiez le fichier `README.md` pour insérer Synapse Proxy dans la section appropriée (ex: **Developer Tools** ou **Observability**).
  3. Respectez le format standard :
     ```markdown
     * [Synapse Proxy](https://github.com/dudutti/synapse-proxy) - Open-source AI gateway providing L1/L2/L3 caching, loop protection, and 14 observability/control tools over MCP.
     ```
  4. Soumettez une Pull Request (PR) avec un message clair.

### 4. MCP.directory
Un catalogue web très propre référençant les outils et serveurs MCP.
* **Procédure** :
  1. Allez sur [mcp.directory](https://mcp.directory/) et cliquez sur le bouton de soumission (Submit).
  2. Remplissez le formulaire en renseignant :
     - Nom : `Synapse Proxy`
     - URL GitHub : `https://github.com/dudutti/synapse-proxy`
     - Description courte, tags (ex: `gateway`, `cache`, `observability`, `security`), et la commande d'exécution.

---

## ✍️ Fiches de Description du Projet (Prêtes à l'emploi)

### Version Française (FR)

* **Nom du Projet** : Synapse Proxy
* **Titre / Slogan** : Gateway IA Open-Source avec Cache L1/L2/L3, Anti-Boucle et Serveur MCP Intégré.
* **Description Courte** :
  Synapse Proxy est une passerelle IA (API Gateway) open-source conçue pour réduire drastiquement vos coûts d'API (OpenAI, Anthropic, etc.) et sécuriser l'exécution de vos agents autonomes. Il intègre un serveur MCP exposant 14 outils d'administration, d'observabilité de cache et de rejeu de session directement dans votre IDE (Cursor, Claude Code, etc.).
* **Description Détaillée** :
  Synapse Proxy optimise le trafic LLM en appliquant trois niveaux de cache :
  - **L1 (Exact)** : Cache de requêtes identiques basé sur des empreintes de hash rapides.
  - **L2 (Sémantique)** : Correspondance vectorielle via un modèle ONNX (MiniLM) local et recherche Redis VSS pour intercepter des requêtes conceptuellement similaires.
  - **L3 (Compression)** : Compression de contexte préservant le préfixe pour réduire les tokens d'entrée.
  - **Pare-feu Agentique** : Détection intelligente des boucles infinies de répétition d'outils et injection de messages d'auto-correction pour corriger les agents à la volée.
  
  Le serveur MCP intégré expose 14 outils pour monitorer le cache, benchmark les modèles (tests A/B), gérer les budgets des clés virtuelles et démarrer/arrêter des sessions d'enregistrement de trafic depuis votre invite de commande d'agent ou votre éditeur.

---

### Version Anglaise (EN - Recommandée pour la publication globale)

* **Project Name** : Synapse Proxy
* **Tagline** : Open-Source AI Gateway featuring L1/L2/L3 caching, Agentic Firewall loop protection, and a built-in MCP server.
* **Short Description** :
  An open-source, high-performance LLM gateway designed to slash API costs and prevent agent loops. Includes an integrated MCP server exposing 14 tools for caching stats, session replay, budget limits, and benchmark management directly to Cursor, Claude Desktop, and compatible IDEs.
* **Detailed Description** :
  Synapse Proxy optimizes AI applications by intercepting LLM traffic and applying three advanced caching layers:
  - **L1 (Exact Match)**: Instant lookup for identical prompt structures.
  - **L2 (Semantic Match)**: Local ONNX-powered multilingual embeddings combined with Redis VSS to resolve conceptually similar queries (e.g. "reset password" vs "forgot password").
  - **L3 (Context Compression)**: Prefix-preserving context pruning to shrink input tokens.
  - **Agentic Firewall**: Live loop detection. Prevents infinite tool-call loops by intercepting repetitive agent outputs and returning self-correction instructions.

  The server implements the Model Context Protocol (MCP), providing 14 administrative and observability tools across Gateway Operations, Session Observability, and Virtual Key Management.

---

## 🛠️ Instructions "How-To" d'Installation et Configuration

### Commande d'exécution (Stdio)
Pour exécuter Synapse Proxy en tant que serveur MCP local :
```bash
# Pour exécuter le binaire directement
./synapse-proxy --mcp --mcp-tier=full
```

### Variables d'environnement nécessaires
Les variables d'environnement suivantes doivent être injectées pour permettre au serveur MCP de se connecter aux bases sous-jacentes (Redis et PostgreSQL) :
* `REDIS_ADDR` : Adresse de Redis Stack (ex: `localhost:6379`).
* `DATABASE_URL` : URL de connexion PostgreSQL (ex: `postgresql://user:pass@localhost:5432/db`).
* `ENCRYPTION_KEY` : Clé de chiffrement hexadécimale de 32 octets.
* `DEFAULT_VIRTUAL_KEY` : La clé virtuelle par défaut utilisée pour router les appels de complétion.

---

### Exemples de configurations d'intégration IDE

#### A. Claude Desktop (Fichier `claude_desktop_config.json`)
Ajoutez ce bloc dans la configuration de Claude :
```json
{
  "mcpServers": {
    "synapse-proxy": {
      "command": "/chemin/absolu/vers/synapse-proxy",
      "args": ["--mcp", "--mcp-tier=full"],
      "env": {
        "REDIS_ADDR": "localhost:6379",
        "DATABASE_URL": "postgresql://optitoken_admin:password@localhost:5432/optitoken_db",
        "ENCRYPTION_KEY": "votre_cle_de_chiffrement_hex_32_bytes",
        "DEFAULT_VIRTUAL_KEY": "sk-opti-votre_cle_virtuelle_active"
      }
    }
  }
}
```

#### B. Cursor (Settings -> Features -> MCP)
1. Cliquez sur **+ Add New MCP Server**.
2. Remplissez les champs :
   - **Name** : `synapse-proxy`
   - **Type** : `command`
   - **Command** : `/chemin/absolu/vers/synapse-proxy --mcp --mcp-tier=full`
3. Ajoutez les variables d'environnement :
   - Cliquez sur "Add Env" pour chaque variable listée dans la section précédente.
