# 🚀 OptiToken Roadmap (V2.0 & Beyond)

Ce document recense les pistes d'amélioration pour les futures versions d'OptiToken. L'accent est mis sur des solutions **réalistes et viables en production sur des serveurs classiques (VPS CPU type Hetzner)**, tout en maintenant les coûts d'infrastructure proches de zéro.

---

## 🎯 1. Nouvelles Fonctionnalités (Viables sur Hetzner)

### 🚦 Smart Routing (Cache Niveau 4)
- **Le Concept :** Un routeur intelligent qui lit la taille et la structure du prompt. Si la tâche est classifiée "Facile" (ex: traduction de 3 phrases, correction orthographique), le proxy réécrit la requête à la volée pour utiliser un modèle très bas coût (ex: `Gemma-2b` ou `GPT-4o-mini`) au lieu du modèle hors de prix demandé par le développeur.
- **Pourquoi sur Hetzner ?** L'algorithme de classification serait écrit en pur Go (analyse Regex/AST ultra-rapide) et consomme 0% de CPU. C'est une simple redirection conditionnelle.

### ⚡ Speculative Streaming (Latence Négative)
- **Le Concept :** Pour les "Matchs Sémantiques" (L2) où la confiance n'est pas de 100% (ex: 80%), le proxy commence à envoyer la réponse cachée à l'utilisateur *en même temps* qu'il interroge la vraie API en arrière-plan. Si l'API confirme, l'utilisateur a reçu son texte avec un **TTFT (Time To First Token) de 0ms**.
- **Pourquoi sur Hetzner ?** Le Go excelle dans le multithreading léger (Goroutines). Gérer deux flux parallèles ne coûte aucune ressource processeur.

### 🔄 Transparent Provider Fallback (Self-Healing)
- **Le Concept :** Si OpenAI tombe en panne (Erreur 503 ou 429), OptiToken reroute instantanément la requête vers Anthropic (Claude 3.5). Mieux encore : OptiToken traduit le flux SSE d'Anthropic pour qu'il mime *exactement* la structure JSON d'OpenAI. Votre application client ne se rend jamais compte que le fournisseur a changé !
- **Pourquoi sur Hetzner ?** Du pur parsing JSON en Go, 0 charge serveur.

---

## 🚀 2. Améliorations de Performance (Architecture)

### 🏎️ gRPC / Protobuf pour l'Embedder ONNX
- **Le Problème :** Actuellement, le Proxy Go discute avec le modèle Python ONNX via des requêtes HTTP JSON. Le JSON est lourd à parser.
- **La Solution :** Remplacer le HTTP par du **gRPC** avec Protobuf. 
- **L'Impact :** On supprime la surchage de sérialisation. Sur Hetzner, cela va libérer du CPU et réduire la latence interne entre le proxy et le modèle de ~15ms à <2ms.

### 📜 Redis Lua Scripting
- **Le Problème :** Le Proxy fait parfois 2 requêtes réseau à Redis (une pour vérifier, une pour écrire).
- **La Solution :** Embarquer des scripts Lua directement dans Redis.
- **L'Impact :** L'opération devient atomique (1 seul aller-retour). Sur un VPS basique, on économise de précieux cycles de connexion TCP.

---

## 🎨 3. Améliorations Design & UX (Dashboard SaaS)

### 🌍 Telemetry Globe (Visualisation Temps Réel)
- **Le Concept :** Sur le Dashboard SaaS, afficher un globe terrestre 3D (avec `Three.js` ou `D3.js`) qui s'illumine à chaque fois qu'OptiToken intercepte une requête dans le monde. Des rayons rouges pour les "Miss", des rayons verts brillants pour les "Cache Hits".
- **L'Impact :** Un effet "Wahou" absolu pour les démonstrations commerciales.

### 🚨 Smart Alerts & Cost Ceilings (Kill Switch)
- **Le Concept :** Permettre de définir une limite de 50$ / jour dans le Dashboard. Si l'API est spammée, le Proxy coupe le robinet (HTTP 429 Too Many Requests).
- **L'Impact :** Une fonction indispensable pour les entreprises qui ont peur des attaques DDoS sur leurs endpoints IA.

---

## ⏸️ 4. Pistes écartées ou reportées (Incompatibles avec Hetzner pur CPU)

Ces idées sont excellentes mais nécessiteraient des instances GPU (AWS/GCP), faisant exploser les coûts d'hébergement.

- ❌ **Active L3 Compression par LLM :** Utiliser un modèle local (ex: Llama-3) pour "résumer" 50 pages de contexte avant de l'envoyer à GPT-4. Sur un CPU Hetzner, la génération prendrait +15 secondes, ruinant la latence.
- ❌ **Visual Semantic Cache (L2 Multi-modal) :** Analyser des images (ex: maquettes UI) pour voir si deux images uploadées sont similaires via le modèle `CLIP`. L'encodage visuel sur CPU est beaucoup trop lent et bloquerait les threads du serveur.
