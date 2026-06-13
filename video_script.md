# 🎬 OptiToken - Script Vidéo & Voix-Off (FR / EN)

Ce document contient les instructions étape par étape pour enregistrer votre vidéo de présentation d'OptiToken, avec le texte exact à lire en voix-off (en Français et en Anglais).

---

## 🛠️ Préparation avant l'enregistrement
1. Assurez-vous que le Dashboard Next.js tourne (`npm run dev`).
2. Assurez-vous que Docker tourne (`docker compose up -d`).
3. Préparez un écran propre (cachez vos favoris/onglets persos).
4. Connectez-vous une première fois pour vérifier que tout marche, puis **déconnectez-vous** pour commencer la vidéo depuis la page de Login.

---

## 🎬 Séquence 1 : Introduction & Dashboard
**Action à l'écran :**
- Vous êtes sur la page `/login`. Connectez-vous avec vos identifiants.
- Arrivée sur le Dashboard, laissez la souris quelques secondes au milieu de l'écran pour que le public admire l'interface (les compteurs qui tournent, les graphes).

🎤 **Voix-off (FR) :**
> "L'ère de l'IA agentique est arrivée, mais avec elle, une facture API (OpenAI, Anthropic) qui explose ! Voici **OptiToken**, un proxy de mise en cache intelligent conçu pour mettre vos agents autonomes au régime. Dès la connexion, notre tableau de bord premium vous montre en temps réel la quantité de requêtes sauvées et l'argent que vous économisez concrètement."

🎤 **Voiceover (EN) :**
> "The era of agentic AI is here, but with it comes an exploding API bill! Meet **OptiToken**, an intelligent caching proxy designed to put your autonomous agents on a diet. Right from login, our premium dashboard shows you real-time metrics on saved requests and the actual money you are keeping in your pocket."

---

## 🎬 Séquence 2 : Configuration & Flexibilité (Settings)
**Action à l'écran :**
- Cliquez sur l'onglet **"Settings"**.
- Scrollez lentement pour montrer les offres Stripe (Hobby, Pro, Enterprise).
- Remontez, montrez vos clés API virtuelles. 
- Jouez légèrement avec le slider de **"Semantic Tolerance"** (Réglez-le par exemple sur `0.15`).

🎤 **Voix-off (FR) :**
> "L'intégration prend 30 secondes. Vous générez une clé virtuelle ici, vous l'utilisez à la place de votre clé OpenAI, et c'est tout. Le véritable pouvoir d'OptiToken réside dans notre moteur sémantique vectoriel. Vous pouvez ajuster la "Tolérance Sémantique" à la volée. Si deux questions ont le même sens, le cache s'active, sans jamais toucher au fournisseur."

🎤 **Voiceover (EN) :**
> "Integration takes 30 seconds. You generate a virtual key here, drop it in instead of your OpenAI key, and you're done. The true power of OptiToken lies in our vector-based semantic engine. You can adjust the Semantic Tolerance on the fly. If two questions mean the same thing, our cache triggers without ever hitting the upstream provider."

---

## 🎬 Séquence 3 : La garantie qualité (Benchmark)
**Action à l'écran :**
- Cliquez sur l'onglet **"Benchmark"**.
- Montrez le tableau de comparaison des réponses originales vs optimisées.
- Montrez avec la souris la colonne **"AI Reliability Score"**.

🎤 **Voix-off (FR) :**
> "Mettre en cache c'est bien, mais comment s'assurer que la réponse servie par le cache sémantique est pertinente ? Nous avons développé le mode Benchmark. Lorsqu'il est activé, notre système interroge quand même l'IA en arrière-plan de manière asynchrone pour évaluer la similarité entre la réponse du cache et la réponse fraîche. Le 'Reliability Score' vous garantit une qualité irréprochable et vous permet d'ajuster finement votre curseur de tolérance !"

🎤 **Voiceover (EN) :**
> "Caching is great, but how do you ensure the semantic cache's response is actually relevant? We developed Benchmark Mode. When enabled, our system asynchronously queries the upstream AI in the background to evaluate the similarity between the cached response and the fresh one. The 'Reliability Score' guarantees pristine quality and lets you confidently fine-tune your tolerance slider!"

---

## 🎬 Séquence 4 : La preuve par le Playground
**Action à l'écran :**
- Cliquez sur l'onglet **"Playground"**.
- Sélectionnez votre modèle (ex: gpt-4o).
- **Étape A :** Tapez la requête : `Write a short poem about space.` et cliquez sur Envoyer.
- **Étape B :** Montrez avec votre souris le petit badge gris **`API Call`** et le temps de latence (ex: `1200ms`).

🎤 **Voix-off (FR) :**
> "Testons-le en direct. J'envoie un prompt totalement nouveau. Comme prévu, c'est un appel API classique, avec un temps de réponse standard."

🎤 **Voiceover (EN) :**
> "Let's test it live. I'm sending a brand new prompt. As expected, it results in a standard API call, with a normal response time."

**Action à l'écran :**
- **Étape C :** Retapez exactement la même chose : `Write a short poem about space.` et envoyez.
- **Étape D :** Montrez avec la souris le badge vert brillant **`Cache Hit`** et le temps éclair (ex: `2ms`).

🎤 **Voix-off (FR) :**
> "Maintenant, je renvoie la même requête. Bam ! Cache Hit instantané. La latence tombe à quelques millisecondes et le coût de cette requête est de zéro."

🎤 **Voiceover (EN) :**
> "Now, I send the exact same request. Boom! Instant Cache Hit. Latency drops to a few milliseconds and the cost of this request is absolutely zero."

**Action à l'écran :**
- **Étape E :** Tapez une phrase différente mais avec le même sens : `Can you give me a small poem regarding the cosmos?` et envoyez.
- **Étape F :** Montrez que le badge est ENCORE **`Cache Hit`** avec un temps éclair (le L2 a fonctionné !).

🎤 **Voix-off (FR) :**
> "Poussons le bouchon plus loin. Je demande la même chose, mais formulé différemment. Grâce à notre modèle d'embedding intégré, OptiToken comprend l'intention et sert le cache sémantique ! Magique."

🎤 **Voiceover (EN) :**
> "Let's push it further. I'm asking the same thing, but phrased differently. Thanks to our built-in embedding model, OptiToken understands the intent and serves the semantic cache! Pure magic."

---

## 🎬 Séquence 5 : La Télémétrie en temps réel (Dashboard)
**Action à l'écran :**
- Cliquez sur le logo **OptiToken** pour retourner au Dashboard principal.
- Descendez jusqu'à la section **"Live Telemetry"**.
- Montrez avec votre souris les logs qui viennent d'apparaître, montrant vos trois requêtes avec les icônes (API Call, L1 Hit, L2 Hit) et les tokens économisés.

🎤 **Voix-off (FR) :**
> "De retour sur le Dashboard, la télémétrie se met à jour en temps réel. Vous visualisez chaque jeton sauvé. OptiToken est le bouclier ultime pour protéger votre budget IA, tout en offrant une expérience ultra-rapide à vos utilisateurs. Rejoignez la révolution de l'optimisation !"

🎤 **Voiceover (EN) :**
> "Back on the Dashboard, telemetry updates in real time. You can visualize every single token saved. OptiToken is the ultimate shield to protect your AI budget, while delivering lightning-fast experiences to your users. Join the optimization revolution!"

---

## 💡 Astuces pour l'enregistrement
- **Logiciels recommandés :** OBS Studio (Gratuit) ou Loom.
- **Rythme :** Ne vous précipitez pas. Laissez à l'audience le temps de lire les réponses générées par le Playground.
- **Zoom :** N'hésitez pas à faire de légers zooms au montage sur le badge "Cache Hit" pour un effet "Waouh" garanti.
