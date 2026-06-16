# Cas d'Usage OptiToken — Comment les équipes économisent vraiment

OptiToken n'est pas un outil unique, c'est une stack de 8 briques qui s'activent selon le contexte. Voici les scénarios concrets où nos utilisateurs voient les plus gros gains, et ceux où OptiToken n'apporte pas grand-chose (on est honnête là-dessus aussi).

---

## 🎯 Le tiercé gagnant : là où OptiToken est imbattable

### Cas #1 — Agent de code autonome sur une longue session (Hermes, Claude Code, OpenClaw)

**Le contexte.** Un dev lance son agent le matin pour construire un dashboard. L'agent va bosser 3-4h, enchaîner 80+ appels, accumuler un historique de conversation qui devient vite colossal. Il lit 30 fichiers, en crée 15, teste 8 endpoints, debug 3 bugs. À chaque tour, il renvoie au LLM l'intégralité de son contexte précédent.

**Ce qu'OptiToken fait :**
- L3 compression : à chaque tour, on élague la chain-of-thought obsolète et les vieux tool outputs. Le prompt envoyé au LLM fait 30-50% de moins que ce que l'agent a construit.
- Loop detection : si l'agent boucle 3 fois sur le même tool call (ça arrive quand l'agent n'a pas compris la sortie d'un tool), on sert la 1ère réponse depuis un cache de loop. Plus de burn de 50 calls identiques.
- Tool dedup : si l'agent relit `package.json` 8 fois dans la session, on détecte et on remonte la métrique. Le dev voit qu'il doit refactorer son agent.
- Compaction hint : on prévient l'agent dans son system prompt que les tool outputs précédents sont résumés. Il apprend à bosser sur les résumés sauf quand il a vraiment besoin du contenu complet.

**Le résultat mesuré sur une vraie session :** agent qui construit un dashboard Next.js complet en partant de zéro = **1 000 000+ tokens purgés** sur la session. Coût final ≈ 30% de ce qu'il aurait été sans OptiToken.

**Le LLM touché :** surtout Claude (Anthropic) et GPT-4o, parce que leur prompt caching 1h reste valide sur la version compressée de notre L3. C'est ça le move : on compresse, et le cache natif du provider fait le reste.

---

### Cas #2 — Bot de service client pour un SaaS multi-tenant

**Le contexte.** Vous vendez un chatbot de support à 50 e-commerces. Chaque e-commerce a son propre contexte (FAQ, politique de retour, catalogue). Vos 50 clients envoient environ 1000 requêtes/jour chacun, dont 60% sont sémantiquement redondantes ("comment retourner", "procédure renvoi", "politique de retour", "je veux rendre mon article" → même intention).

**Le piège à éviter.** Si vous isolez le cache par user (le `user` envoyé dans le body OpenAI = l'end-user qui parle au chatbot), vous obtenez 50 000 micro-buckets de cache avec ~1 requête par bucket. Le hit rate tombe à 0. Si vous n'isolez pas du tout, MegaShoes et BellePile partagent le même cache et vous avez une fuite de données : "où est ma commande #1234" renvoie la commande du voisin.

**Ce qu'OptiToken fait — scope dynamique automatique.** Le proxy classifie chaque requête en 3 scopes, **sans rien demander au client** :

| Scope | Namespace Redis | Exemples de requêtes | Partagé entre |
|---|---|---|---|
| `personal` | `optitoken:l1cache:vk={vk}:user={userId}:scope=personal` | "où est ma commande #1234", "mon compte est bloqué", "modifie mon adresse" | jamais (1 user = 1 bucket) |
| `business` | `optitoken:l1cache:vk={vk}:scope=business` | "politique de retour MegaShoes", "livraison en Corse", "comment contacter le support" | tous les end-users du **même** e-commerce |
| `generic` | `optitoken:l1cache:vk={vk}:scope=generic` | "comment faire une boucle Python", "capitale de la France" | tous les e-commerces (c'est le cache global) |

La classification est faite côté proxy en < 1ms par une combinaison de regex PII (détection de numéros de commande, paths personnels, IDs, credentials) + heuristiques linguistiques (questions FAQ vs contextuelles). Zéro coût LLM, zéro appel supplémentaire.

**Le résultat.**
- L1 exact catch les politesses et questions FAQ identiques (`scope=business` ou `scope=generic` selon le contexte) → réponse en 2ms
- L2 sémantique catch les reformulations de questions business → réponse en 40ms avec cosine match
- Les questions personnelles (`scope=personal`) ne sont jamais partagées → impossible d'avoir une fuite de données
- Les questions business de MegaShoes sont partagées entre **tous les end-users de MegaShoes** mais jamais avec BellePile

**Chiffre réaliste.** Sur les 50 e-commerces, si 40% des questions sont FAQ business (politique, livraison, retours) et 20% sont génériques (math, culture, code), tu as 60% du flux qui passe par les scopes partagés. **Économie : 40-60%** sur ce flux partageable. Le 40% de questions personnelles (commandes, comptes) ne bénéficient pas du cache mais ne coûtent rien de plus.

**Limite assumée.** Si MegaShoes a 1 user qui pose la même question 100 fois (un power user qui retape), OptiToken catch bien via L1 exact. Si MegaShoes a 1000 users uniques qui posent chacun la même question 1 fois, le L2 sémantique catch quand même (similarité d'intention, pas identité exacte). En dessous de ~3 users actifs par tenant, le hit rate business est trop faible pour être significatif.

**Ce que OptiToken ne fait pas (encore).** Le client n'a pas la main sur le scope — il est déduit automatiquement. Si tu as des cas où tu sais qu'une question est partageable (par exemple un user admin qui pose des questions de config) et qu'OptiToken la classe comme `personal`, tu dois attendre qu'on ajoute un header `X-Optitoken-Scope` que le client peut forcer. C'est dans notre backlog.

---

### Cas #3 — Agent qui boucle suite à un bug

**Le contexte.** Vous avez un agent en prod qui, à cause d'un tool mal configuré, fait 30 tool calls identiques en 2 minutes avant de crasher. Vous vous en rendez compte le lendemain en voyant la facture. Coût : $47 pour rien.

**Ce qu'OptiToken fait :**
- Loop detection intercepte la 3ème requête identique en moins de 60s et la sert depuis le cache de loop.
- À partir de la 4ème, c'est instantané (pas même de hash, on check juste le compteur).
- Le dashboard affiche "🔁 LOOP DETECTED — agent X a fait 30 requêtes identiques sur 2 minutes".

**Le résultat :** la facture passe de $47 à $0.20. Et surtout, vous voyez l'incident en temps réel dans le dashboard au lieu de le découvrir sur la facture du mois suivant.

**Variante "boucle lente" :** 3 requêtes identiques en 5 minutes. Notre seuil actuel est 60s, donc on ne l'attrape pas. C'est dans notre backlog : passer à une détection basée sur la similarité sémantique + fenêtre glissante, pas juste l'égalité de hash.

---

## 🟢 Le tiercé solide : gains réguliers mais moins spectaculaires

### Cas #4 — Cron job de génération de rapports

**Le contexte.** Un script cron lance tous les matins à 8h00 la même requête : "résume-moi l'état du marché avec ce JSON en input". Parfois le script bug et se relance 4 fois de suite dans la même minute.

**Ce qu'OptiToken fait :** L1 exact catch les 3 relances. Coût évité : 3 appels LLM = ~$0.15 par jour = $55/an.

Pas révolutionnaire, mais c'est du passif : tu l'installes une fois, ça économise sans que tu touches à rien.

---

### Cas #5 — Dev qui teste son prompt en local

**Le contexte.** Tu es dev, tu itères 50 fois sur un prompt pendant 1h pour trouver la bonne formulation. Les 50 requêtes sont presque identiques (tu changes un mot à chaque fois).

**Ce qu'OptiToken fait :** L2 sémantique détecte que 80% de tes requêtes sont équivalentes. Les 10 plus pertinentes sont servies depuis le cache (tu vois la dernière réponse correspondante), les 40 autres passent au LLM.

**Le résultat :** feedback loop plus rapide (2ms au lieu de 2s sur les hits), coût de test réduit.

---

### Cas #6 — App qui fait des embeddings en batch

**Le contexte.** Tu vectorises 10K documents par jour pour alimenter un RAG. Beaucoup de documents se répètent (templates, footers, headers).

**Ce qu'OptiToken fait :** L1 exact catch les doublons. Si tu as 20% de doublons, c'est 20% d'économies directes sur les embeddings.

**Bonus :** L2 sémantique attrape les "presque doublons" (deux paragraphes qui disent la même chose avec des mots différents). Pour du RAG sur de la doc d'entreprise, ça peut monter à 40% d'économies.

---

## 🟡 Le tiercé contextuel : ça dépend

### Cas #7 — App conversationnelle simple (1 appel, prompt court, pas d'historique)

**Le contexte.** Un chatbot de FAQ, un outil de reformulation, un générateur de description produit. Chaque appel est isolé, prompt < 1K tokens, pas d'agentic loop.

**Ce qu'OptiToken fait :** pas grand-chose. Le L1 ne catch rien (pas de doublons exacts). Le L2 catch quelques reformulations sémantiques (~10-20% du flux). Le L3 n'a presque rien à compresser.

**Verdict :** installe OptiToken si tu veux l'observabilité et la gouvernance (savoir qui consomme quoi, mettre des budgets par user), mais ne t'attends pas à 50% d'économies. Réalité : 5-15%.

**C'est honnête :** on n'est pas pertinents sur ce cas. Les providers (Anthropic, OpenAI) ont leur propre prompt caching qui fait aussi bien pour 0 effort.

---

### Cas #8 — Modèle local (Ollama, LM Studio)

**Le contexte.** Tu fais tourner un Llama 3 en local. Pas de facture API.

**Verdict :** OptiToken ne t'apporte rien économiquement. Mais il apporte l'observabilité : tu vois quels prompts passent, combien de tokens, quelle latence, quel taux de cache. Si tu utilises Ollama pour prototyper avant de basculer sur un provider payant, OptiToken te donne les métriques pour prédire ta facture.

---

## 🔴 Le tiercé perdant : là où on n'est pas (encore) bon

### Cas #9 — Agents qui font du sub-agent fan-out

**Le contexte.** Un orchestrateur qui spawn 10 sous-agents en parallèle, chacun sur un sub-task. Les 10 sous-agents ont des contextes différents mais leur meta-prompt (instructions système) est identique.

**Ce qu'OptiToken fait :** compresse chaque requête individuellement. Pas de coordination cross-agent.

**Ce qu'on ne fait pas (encore) :** détecter que 5 sous-agents font la même chose et mutualiser. Token-Optimizer fait ça implicitement via le subagent cost breakdown. Nous on est encore "1 proxy = 1 requête".

**Workaround :** si tous les sub-agents passent par OptiToken avec le même virtual key, le L1 catch naturellement les meta-prompts identiques. Mais c'est du bonus, pas une feature.

---

### Cas #10 — Voice-to-voice temps réel

**Le contexte.** App de conversation orale avec un LLM. Latence < 300ms obligatoire.

**Verdict :** OptiToken ajoute 50-150ms de latence (parsing JSON, hash SHA-256, check Redis, vector search éventuel). Sur du streaming avec time-to-first-token critique, c'est rédhibitoire.

**Workaround :** on a un mode "bypass" qui désactive tout le cache/compression pour les requêtes tagged `X-Bypass-Cache: true`. Tu peux l'activer par clé API pour les workloads temps réel. Mais tu perds tous les bénéfices.

---

## 📊 Récap par profil d'utilisateur

| Profil | Économie attendue | Setup | Cas d'usage |
|---|---|---|---|
| **Startup avec 1 agent coding** | 60-80% | 5 min | #1 |
| **SaaS B2B avec chatbot multi-tenant** | 40-60% (sur le flux partageable) | 30 min | #2 |
| **Entreprise avec 10+ agents en prod** | 50-70% | 1h + gouvernance | #1, #2, #3, #9 |
| **Dev qui itère sur des prompts** | 10-20% | 5 min | #5 |
| **App simple sans agent** | 5-15% (ou 0%) | 5 min | #7 |
| **Voice/streaming temps réel** | Négatif (latence) | Mode bypass | #10 |

---

## Le cas spécial : combiner OptiToken + Token-Optimizer

Si tu utilises déjà Token-Optimizer (le plugin Claude Code), OptiToken n'est pas en concurrence. Les deux ensemble :

- Token-Optimizer réduit la **qualité du contexte** (élague les skills jamais utilisés, compact intelligemment)
- OptiToken réduit le **coût du transport** (cache, compression, routing)

Résultat : tu peux atteindre 95%+ d'économies totales. Token-Optimizer est gratuit, OptiToken est open-source, tu peux donc tester la combinaison sans risque.

C'est probablement la configuration la plus rentable pour 2026.
