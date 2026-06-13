# 🎬 OptiToken - Scripts Vidéo de Démonstration (Série de 3 vidéos)

Voici la structure de vos 3 nouvelles vidéos de démonstration complètes, pensées pour explorer **100% des fonctionnalités d'OptiToken**. Ces vidéos peuvent être publiées sous forme de série (Part 1, 2, 3) ou compilées en une "Masterclass" de 7 à 8 minutes.

---

## 🎥 Vidéo 1 : La Puissance du Cache Sémantique (Durée : ~2m30)

**Objectif :** Présenter le produit, le Dashboard global, et la magie du Playground (L1 & L2).

### 1. Introduction & Dashboard Global
**Action à l'écran :**
- Connectez-vous depuis la page de Login avec le compte **`test@example.com`** et atterrissez sur le Dashboard principal. Laissez la souris parcourir les jauges supérieures.
- Descendez jusqu'au tableau "Live Telemetry" pour montrer les logs en temps réel.

🎤 **Voix-off :**
> "Bienvenue dans OptiToken, le premier proxy intelligent conçu spécifiquement pour réduire les factures astronomiques de l'IA Agentique. L'idée est simple : vous remplacez votre clé OpenAI par notre clé virtuelle, et OptiToken agit comme un bouclier.
> Sur ce Dashboard, vous voyez en temps réel vos économies. Les requêtes interceptées apparaissent dans ce grand tableau de télémétrie, où chaque token évité est immédiatement converti en dollars sauvés."

### 2. Le Playground (Test L1)
**Action à l'écran :**
- Allez dans l'onglet **Playground**. Sélectionnez votre modèle préféré.
- Tapez : `Explique-moi la théorie de la relativité en 3 phrases simples.`
- Envoyez. Montrez la latence (ex: 1800ms) et le badge gris **`API Call`**.
- Renvoyez **exactement la même requête**. Montrez le badge vert **`L1 Cache Hit`** et la latence éclair (ex: 5ms).

🎤 **Voix-off :**
> "Allons dans le Playground pour voir ça en action. Je pose une question complexe. Le proxy fait un appel standard à l'API. La latence est normale. 
> Maintenant, un autre de vos utilisateurs pose exactement la même question. Magie : OptiToken sert la réponse directement depuis sa mémoire cache (L1). La latence est foudroyante, à quelques millisecondes, et le coût de l'appel API est de zéro absolu."

### 3. La vraie magie : Le L2 (Semantic Hit)
**Action à l'écran :**
- Tapez une phrase différente mais au même sens : `Peux-tu résumer la relativité générale en trois phrases faciles à comprendre ?`
- Envoyez. Montrez le badge **`L2 Cache Hit`** et la latence (ex: 80ms).

🎤 **Voix-off :**
> "Mais la vraie révolution d'OptiToken, c'est son niveau 2 : le cache sémantique vectoriel. Je pose la même question, mais formulée complètement différemment, avec d'autres mots. En arrière-plan, notre modèle ONNX intégré analyse l'intention. Résultat ? Un Cache Hit L2. L'intention est la même, l'IA ne recalcule rien. C'est l'arme absolue pour les agents conversationnels et les bots de support !"

---

## 🎥 Vidéo 2 : Le Mode Benchmark & Le LLM Judge (Durée : ~3m00)

**Objectif :** Prouver la fiabilité du cache sémantique et montrer comment les agents évitent les boucles d'outils inutiles.

### 1. Le problème de la fiabilité
**Action à l'écran :**
- Rendez-vous sur l'onglet **Benchmark**. Affichez le grand écran partagé (Split View).

🎤 **Voix-off :**
> "C'est génial de servir une réponse en cache, mais comment s'assurer que cette réponse est toujours pertinente et de bonne qualité ? 
> C'est là qu'intervient le mode exclusif Benchmark d'OptiToken. En arrière-plan, notre système exécute une 'Requête de Contrôle' invisible, sans OptiToken, pour comparer ce qui se serait passé."

### 2. L'A/B Testing et l'Évaluation
**Action à l'écran :**
- Scrollez pour montrer les panneaux "CONTROL" (rouge) vs "TEST" (vert).
- Déroulez les "Raw Prompt" et "Optimized Prompt" pour montrer les différences de tokens.
- Insistez sur le bas de la page : le **LLM Judge Feedback** et le **AI Reliability Score** de 95% ou 100%.

🎤 **Voix-off :**
> "Sur ce tableau de bord d'A/B Testing, vous voyez la requête originale à gauche, et l'action d'OptiToken à droite. Mais observez le bas de l'écran : le 'LLM Judge'.
> OptiToken utilise un LLM juge indépendant pour lire les deux réponses et certifier que la réponse servie par le cache est parfaitement adaptée. Le 'Reliability Score' vous apporte une tranquillité d'esprit absolue sur la qualité de votre produit en production."

### 3. Les boucles d'outils (Tool Calls)
**Action à l'écran :**
- Montrez une entrée où la requête contrôle ("Sans OptiToken") montre un appel d'outil (ex: `web_search`), tandis que la réponse OptiToken affiche une réponse texte finale.

🎤 **Voix-off :**
> "Et regardez ce cas fascinant. Sans OptiToken, le LLM s'apprêtait à déclencher un appel d'outil pour chercher sur le web, entraînant une longue boucle coûteuse en API. 
> Grâce à l'optimisation sémantique d'OptiToken, l'agent a compris le contexte compressé et a donné la réponse directement. Le proxy ne se contente pas de cacher des requêtes, il rend vos agents plus intelligents en évitant l'effet 'lapin dans les phares'."

---

## 🎥 Vidéo 3 : Le Playground Split & Configuration Avancée (Durée : ~2m30)

**Objectif :** Explorer la vue double écran (A/B testing en direct) et les paramétrages techniques.

### 1. Le Playground Split
**Action à l'écran :**
- Rendez-vous sur l'onglet **Playground Split**.
- Tapez une requête dans la barre du haut et validez. Les deux fenêtres (gauche/droite) se remplissent simultanément. L'une via l'API directe, l'autre via OptiToken.

🎤 **Voix-off :**
> "Si vous doutez encore de la puissance d'OptiToken, notre Playground Split est le terrain de jeu idéal pour les développeurs. Il vous permet de lancer la même requête simultanément sur votre fournisseur habituel, et à travers OptiToken.
> Les résultats apparaissent côte-à-côte, avec les latences réelles et les compteurs de tokens. Vous pouvez littéralement voir la différence de vitesse à l'œil nu."

### 2. Les Settings (La salle des machines)
**Action à l'écran :**
- Cliquez sur l'onglet **Settings**.
- Survolez la gestion des clés API LLM (Google, Anthropic, MiniMax, OpenAI).
- Concentrez-vous sur le slider de **Semantic Tolerance**.

🎤 **Voix-off :**
> "Entrons dans la salle des machines. C'est ici que vous définissez vos vraies clés d'API fournisseurs, car OptiToken gère le routage multi-modèles (OpenAI, Anthropic, Deepseek...).
> Le réglage clé, c'est ce curseur de 'Tolérance Sémantique'. C'est vous qui décidez de la souplesse du cache L2. Un score faible oblige l'utilisateur à poser la question de manière presque identique. Un score plus élevé permet d'attraper de larges variations de langage. Couplé au LLM Judge du mode Benchmark, vous pouvez affiner ce curseur à la perfection pour votre application."

### 3. Conclusion Finale
**Action à l'écran :**
- Revenez sur le Dashboard principal et zoomez doucement sur le compteur "$ Saved".

🎤 **Voix-off :**
> "En moins de 5 minutes d'intégration, OptiToken transforme vos flux agentiques imprévisibles en coûts maîtrisés et en latence imperceptible. Prenez le contrôle de votre facture API, et passez à l'ère de l'optimisation intelligente."
