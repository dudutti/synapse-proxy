# 🎬 Scripts et Prompts pour la Vidéo Démo

Voici la séquence exacte à enregistrer pour avoir une démo percutante. Assure-toi que le mode Benchmark est **activé** sur ta clé virtuelle.

## 🎯 Prompt 1 : Cache Miss (Baseline / Requête Initiale)
*Explication dans la vidéo : "Voici une requête complexe. C'est la première fois qu'OptiToken la voit, elle va donc être envoyée au fournisseur LLM."*

**Copie/Colle ceci dans le Playground :**
> Peux-tu analyser en détail l'impact de l'intelligence artificielle sur l'économie spatiale ? Je veux que tu couvres :
> 1. La navigation autonome des sondes.
> 2. L'analyse des données satellites pour l'agriculture terrestre.
> 3. La gestion des débris spatiaux via des modèles prédictifs.
> Fais une synthèse de 5 paragraphes bien structurés, avec une conclusion sur les perspectives de 2030.

*Ce qu'il se passe :*
- Le proxy envoie la requête à l'API.
- **Résultat :** Latence longue (ex: 2000-5000ms), 0 Token économisé. Type : `Standard Routing`.

---

## ⚡ Prompt 2 : L1 Cache Hit (Match Exact)
*Explication dans la vidéo : "Maintenant, renvoyons EXACTEMENT la même requête. Une autre application, ou un autre utilisateur, pose la même question."*

**Copie/Colle EXACTEMENT le même prompt :**
> Peux-tu analyser en détail l'impact de l'intelligence artificielle sur l'économie spatiale ? Je veux que tu couvres :
> 1. La navigation autonome des sondes.
> 2. L'analyse des données satellites pour l'agriculture terrestre.
> 3. La gestion des débris spatiaux via des modèles prédictifs.
> Fais une synthèse de 5 paragraphes bien structurés, avec une conclusion sur les perspectives de 2030.

*Ce qu'il se passe :*
- OptiToken reconnaît le hash SHA-256.
- **Résultat :** Latence instantanée (ex: 2-10ms), 100% des tokens (Input et Output) économisés. Type : `Cache Hit (L1)`.

---

## 🧠 Prompt 3 : L2 Cache Hit (Match Sémantique)
*Explication dans la vidéo : "C'est ici que la magie opère. Que se passe-t-il si un utilisateur pose la MÊME question, mais avec des mots différents et des fautes de frappe ?"*

**Copie/Colle cette version reformulée :**
> Salut, j'ai besoin d'une analyse sur le rôle de l'IA dans l'économie de l'espace. Parle-moi de la navigation des sondes toute seule, de la façon dont les satellites aident les champs sur terre, et comment on évite les déchets spatiaux avec des prédictions.
> Fais-moi 5 paragraphes et conclus sur 2030 stp.

*Ce qu'il se passe :*
- L'Embedder ONNX génère un vecteur, cherche dans Redis, et trouve une distance Cosinus très faible (< 0.15).
- **Résultat :** Latence très courte (ex: 50-100ms), 100% des tokens économisés. Type : `Cache Hit (L2)`.

---

## 🛠️ Prompt 4 : Démonstration du Benchmark (A/B Testing)
*Explication dans la vidéo : "Allons voir comment l'IA juge notre Cache Sémantique en arrière-plan !"*

**Action :** 
1. Va dans l'onglet **Benchmark**.
2. Montre le log généré par le Prompt 3.
3. On y voit l'écran divisé : 
   - **Control** (Requête qui aurait dû être envoyée, avec sa latence simulée élevée).
   - **OptiToken** (La réponse servie par le cache L2, avec sa latence quasi-nulle).
   - Le **LLM Judge Feedback** (en bas) qui donne un score de fiabilité (ex: 95%) certifiant que la réponse du cache répondait parfaitement à la requête reformulée !
   - Les économies distinctes **Input / Output Billed** (ex: 0 / 0 pour OptiToken !).

---

## 🗜️ Bonus : L3 Cache Hit (Compression Contextuelle)
*Explication : "Le niveau 3 (L3) s'active automatiquement sur les longues conversations, en purgeant les pensées internes (CoT) et les logs d'outils inutiles."*

*(Le Playground actuel n'envoie qu'un seul message à la fois pour tester le L1/L2, donc le L3 se teste typiquement via cURL ou dans le backend de l'entreprise)*.
Pour le voir apparaître dans ton Dashboard, tu peux lancer cette commande dans un second terminal :

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer VOTRE_VIRTUAL_KEY" \
  -d '{
    "model": "MiniMax-M2.7",
    "messages": [
      {"role": "user", "content": "Quelle est la capitale de la France ?"},
      {"role": "assistant", "content": "<thought>Le user demande la capitale. Je cherche. Cest Paris.</thought> La capitale de la France est Paris."},
      {"role": "user", "content": "Et celle de lItalie ?"},
      {"role": "assistant", "content": "<thought>Italie = Rome.</thought> Cest Rome."},
      {"role": "user", "content": "Merci, fait un résumé."}
    ]
  }'
```
*Le proxy détectera les `<thought>` obsolètes, compressera le payload (passant par exemple de 200 à 50 tokens d'input), et loggera une économie `L3` !*
