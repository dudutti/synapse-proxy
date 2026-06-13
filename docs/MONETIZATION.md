# Monetization Strategy

OptiToken is positioned as a critical infrastructure layer for AI-native companies. To maximize adoption while generating revenue, an **Open Core (SaaS)** model is highly recommended.

## Why Open Core?
Companies are highly reluctant to route their sensitive AI prompts and proprietary API keys through a closed-source third-party proxy. By open-sourcing the Data Plane, you build trust.

1. **Open Source (GitHub):** The Go Proxy, Redis VSS integration, and ONNX embedder are open-source. Developers can self-host the core proxy via CLI or Docker Compose.
2. **Closed Source (SaaS):** The beautiful Next.js Dashboard, team management, unified billing, advanced analytics, and 1-click cloud deployments are proprietary and hosted by you.

## Revenue Models

### 1. The SaaS Tiered Subscription (Recommended)
Do not charge per API request. Instead, charge based on **Requests Per Second (RPS) limits** and **Seats/Features**, similar to Vercel or Supabase.

* **Hobby (Free):** Up to 10 RPS, 1 User, 7-day log retention.
* **Pro ($49/month):** Up to 100 RPS, 5 Users, 30-day log retention, Benchmark Mode included.
* **Enterprise ($499+/month):** Custom RPS, SSO, self-hosted proxy syncing with cloud dashboard, dedicated support.

### 2. Unified Billing avec "Win-Win Split" (La solution idéale)
Vous avez soulevé un excellent point : si on facture 100% du prix et qu'on garde toute la marge, l'utilisateur n'a aucun intérêt financier à utiliser OptiToken (à part le gain de latence). La meilleure approche est de diviser les économies en deux (50% pour vous, 50% pour l'utilisateur).

**Le problème de la facturation a posteriori :**
Facturer "à la fin du mois sur les économies" est dangereux (risque d'impayés, complexité de facturation). La solution est le système de **Crédits Prépayés avec Déduction Dynamique** :

* **Mécanisme :** Le client achète $100 de "Crédits OptiToken" à l'avance (pas de risque de fraude).
* **Requête standard (Miss) :** Si la requête coûte $0.01 chez OpenAI, vous déduisez exactement $0.01 de son solde de crédits.
* **Requête cachée (Hit) :** La requête ne vous coûte rien (cache). Au lieu de déduire $0.01, vous déduisez seulement **$0.005** (la moitié).
* **Résultat :** Le client voit immédiatement qu'il paie 50% moins cher sur ces requêtes. Son solde descend plus lentement. Et vous encaissez $0.005 de marge pure à chaque Cache Hit ! Tout le monde est gagnant instantanément, et vous êtes payé en avance.

### 3. Sell the "Enterprise" Self-Hosted License
For large corporations that want the Dashboard and Analytics entirely on-premise (air-gapped), sell an Enterprise license key for $10,000/year.
