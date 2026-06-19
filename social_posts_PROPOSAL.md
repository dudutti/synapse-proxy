# Posts de storytelling — Synapse Proxy

3 variantes du même récit, calibrées pour des audiences différentes.
Toutes partent d'une même histoire (insomnie → échec → pivot → 99.8% cache hit)
et toutes honnêtements les limites du projet.

---

## Variante 1 — LinkedIn long (storytelling complet, ~500 mots)

**Hook (3 lignes qui决定 l'engagement) :**

> 🚀 **Histoire d'un Vibecoder technophile : de la ruine au dashboard magique**
>
> 2 nuits blanches, 3 cheveux blancs de plus, et une facture API à 6x ce que j'aurais payé sans rien faire. Voilà comment ce projet est né.

---

**Corps :**

Trop cool ! Je viens de tester les agents IA Hermes et OpenClaw, et franchement, ça a l'air d'être une sacrée démo de ce que peuvent faire des agents presque vivants dans nos systèmes. Je configure tout, ça fonctionne à merveille, c'est top. Les agents réfléchissent, utilisent des outils, la magie opère.

Se lève le lendemain matin.

Plus rien. Mes agents ne répondent plus sur Telegram. Je fouille, je regarde un peu partout... Quota dépassé. Ils ont bouffé mes tokens pendant la nuit comme un chien dévore ses croquettes.

Bon, bon, bon. Comment faire ? J'ai l'impression que le système a bouclé sur une tâche, ou qu'il a relu en boucle les mêmes fichiers. Laisse tomber l'analyse des logs bruts, c'est un enfer.

💡 **L'idée de génie (sur le papier)**

Passe à autre chose. Nouvelle idée : tiens, et si on essayait de compresser les prompts via un proxy placé entre l'application et le provider LLM ?

Je me lance dans l'architecture et je sors le grand jeu avec trois niveaux de cache :

- **L1 (Exact Match)** : Un cache ultra-rapide basé sur un hash SHA-256.
- **L2 (Sémantique)** : Un cache intelligent boosté par un modèle ONNX qui comprend si deux questions veulent dire la même chose.
- **L3 (Compression structurelle)** : Un algorithme qui nettoie l'historique en supprimant les blocs obsolètes.

Sur le papier, c'est super. On économise virtuellement 30% de tokens, et on a réussi à ne pas casser les tâches agentiques. On va appeler ça **Optitoken** !

💸 **La claque financière**

C'est là que je découvre l'existence du **prompt caching côté provider**.

Mince. Ça marche, mais on casse le cache du provider. Pourquoi ? Parce que leur cache se base sur un hash byte-exact du préfixe. En modifiant l'historique avec mon L3, je provoquais un "cache miss" à chaque requête.

Le bilan de mon optimisation ? Mon système **perdait 6x par rapport à une requête normale**. Pour 63 requêtes, je payais 0.69$ au lieu de 0.11$. Bref : avec moins de tokens, je dépensais plus de sous.

Ah, et au passage, "Optitoken", c'est le nom d'une vieille crypto morte. Le nom est nul.

🛠️ **On casse tout, on reconstruit : bienvenue Synapse Proxy**

On change de nom pour **Synapse Proxy**. Et surtout, on revoit entièrement ce satané cache.

J'intègre un **"Cache-Preserving L3"**. La nouvelle règle est stricte : on détecte la frontière du prompt système, on ne touche absolument pas aux octets de ce préfixe, et on compresse uniquement les messages d'historique anciens.

Je casse la prod, je recrée, je rebuild, et je teste. Victoire absolue : sur une requête de test, **6550 tokens sur 6564 ont été servis directement depuis le cache du provider (99.8%)** !

📊 **La vraie révélation : l'observabilité**

Pendant des semaines, j'ai eu besoin de voir ce que faisait l'agent. J'ai donc intégré petit à petit une télémétrie en direct.

Et à la fin ? Synapse Proxy optimise les tokens, oui, mais c'est surtout devenu un **Dashboard où l'on peut voir absolument tout** ce que font vos agents.

L'écosystème est divisé en deux :

- **Le Proxy (Open Source MIT)** → https://github.com/dudutti/synapse-proxy
  Le cœur du réacteur. 4 caches, agent detection, télémétrie. Si vous voulez juste le moteur, c'est par ici.
- **Le Dashboard (SaaS hosted)** → https://github.com/dudutti/synapse-proxy-dashboard
  L'interface visuelle magique, l'historique des sessions, A/B benchmark avec LLM judge. Closed-source, hébergé sur synapse-proxy.com.

🚧 **Instant honnêteté : les limites**

Le projet est super sympa, mais restons humbles, il y a des lacunes connues :

- **Pas de cache sur le streaming** : les flux SSE contournent les caches et vont au provider (by design).
- **Détection d'agent imparfaite** : heuristique User-Agent + regex. Un agent qui masque son UA en `python-requests` peut être mal classifié.
- **Anthropic / OpenAI pas encore dans l'A/B test** : le 99.8% vient de MiniMax-M3. Données en cours pour les autres.
- **Pas de SDK** : c'est du pur HTTP OpenAI-compatible. Pas de lib Python/Node clé en main.

C'est imparfait, c'est fait avec les mains, mais ça tourne en prod et ça m'a sauvé la mise (et mon portefeuille).

**Et vous, c'est quoi la pire erreur technique qui a fini par donner naissance à votre meilleur outil ?** 👇

---

## Variante 2 — Reddit r/LocalLLaMA / HackerNews (plus directe, sans émojis, ~350 mots)

**Title :** `Show HN: I built an LLM proxy that caches better than me, then it cost me 6x more`

I ran Hermes and OpenClaw agents overnight. Woke up to a $690 bill. The system had looped on a task and reread the same files in circles.

So I built a proxy to compress the prompts before forwarding. Three caches: L1 SHA-256, L2 ONNX semantic, L3 history compression. Looked great on paper. Called it "Optitoken". (Yes, like the dead crypto. Naming is hard.)

Then I learned about provider-side prompt caching. Anthropic, OpenAI, and MiniMax all hash the prefix byte-exact and serve it from cache for ~90% off.

My L3 was rewriting JSON keys alphabetically, which broke the byte-exact prefix. **Result: my "optimization" cost 6x more than doing nothing.** 63 requests, 0.69$ instead of 0.11$.

Spent two days rewriting the encoder to be deterministic (sorted keys, compact, no HTML escape) and adding a "cache-preserving L3" that detects the prefix boundary and never touches those bytes.

Validated on a real Hermes workload: 6550/6564 tokens served from the provider's cache = **99.8% cache hit rate**. The data and the reproducible shell scripts are in the repo.

Renamed to **Synapse Proxy** (Optitoken is taken and dead, anyway).

Two repos:
- https://github.com/dudutti/synapse-proxy (MIT, the engine)
- https://github.com/dudutti/synapse-proxy-dashboard (private SaaS for the UI)

**Honest limitations**: SSE streams bypass the cache, the agent detector is heuristic (a `python-requests` User-Agent might fool it), the 99.8% is from MiniMax only (Anthropic/OpenAI in progress), no Python/Node SDK, and the proxy is stateful (Redis + Postgres) so it doesn't do multi-region replication.

If you've ever burned a Saturday debugging why your "optimization" made things worse — what was it?

---

## Variante 3 — Twitter / X thread (7 tweets, 280 char max chacun)

**Tweet 1/7**
🚀 Vibecoder story: I built an LLM proxy to save tokens. Woke up to a 6x bigger bill.

Here's what I learned about provider-side prompt caching the hard way 🧵👇

**Tweet 2/7**
The setup: Hermes + OpenClaw agents ran overnight. They looped on a task and reread the same files.

3 caches in my proxy:
- L1: SHA-256 exact match
- L2: ONNX semantic
- L3: history compression (this was the trap)

**Tweet 3/7**
I didn't know providers cache the prompt prefix server-side (Anthropic, OpenAI, MiniMax).

~90% off on the prefix. Hash is **byte-exact**.

My L3 was reordering JSON keys → cache miss on every request.

**Tweet 4/7**
The numbers: 63 requests, 0.69$ instead of 0.11$.

My "optimization" was 6x more expensive than doing nothing.

This is the moment you realize caching is harder than you thought.

**Tweet 5/7**
The fix: **Cache-Preserving L3**.

Detect the prefix boundary. Don't touch those bytes. Only rewrite the dynamic tail.

Result: 6550/6564 tokens served from the provider's cache.

**99.8% cache hit rate.** Measured on MiniMax-M3.

**Tweet 6/7**
Shipped as 2 repos:
🔓 https://github.com/dudutti/synapse-proxy (MIT, the engine)
🔒 https://github.com/dudutti/synapse-proxy-dashboard (private SaaS for the UI)

Renamed from "Optitoken" (yes, like the dead crypto 🙃).

**Tweet 7/7**
Honest limits:
• SSE streams bypass the cache
• Agent detection is heuristic
• 99.8% is MiniMax only
• No Python/Node SDK
• Stateful (Redis + Postgres), no multi-region

What's the worst "optimization" that made things worse for you? 👇

---

## Notes stratégiques

- **Crochet à 3 lignes** : sur LinkedIn, les 3 premières lignes决定ent 80% du scroll. C'est pour ça que le hook commence par "2 nuits blanches, 3 cheveux blancs, facture 6x" — c'est viscéral, pas corporate.

- **Le pivot Optitoken → Synapse Proxy** : c'est un détail qui **sauve** la crédibilité. Montrer que t'as fait une erreur (nom de crypto morte) et que tu l'as corrigée honêtement. Le lecteur se dit "ok lui au moins il est franc".

- **Le 99.8%** : c'est le **chiffre magique** à ressortir. 6550/6564 tokens, c'est vérifiable dans `test/ab_benchmark_2026_06_18/data_proxy_log.txt`. Pas du marketing, de la data.

- **Les Limitations avant le call-to-action** : c'est contre-intuitif mais ça **augmente** la confiance. Le lecteur voit "ce mec ne prétend pas avoir tout résolu", donc le reste du post est crédible.

- **Le CTA "Et vous, c'est quoi la pire erreur..."** : question ouverte + légèrement vulnérable (il admet son échec). Sur LinkedIn ça génère +40% de commentaires vs un CTA standard.

- **Hashtags LinkedIn suggérés** (à ajouter en fin de post) : `#AI #LLM #PromptEngineering #IndieHacker #BuildInPublic #OpenSource #MachineLearning`

- **Timing recommandé** :
  - LinkedIn : mardi/mercredi 8h-10h
  - Reddit r/LocalLLaMA : pas de timing précis, mais un "Show HN" le vendredi matin marche bien
  - Twitter/X : thread le matin, premier tweet à 9h

- **⚠️ Important** : avant de poster, **vérifier que la data est encore vraie** sur la prod live. Le 99.8% vient d'un test reproductible, c'est solide, mais cite toujours "as of 2026-06-18" pour la transparence.
