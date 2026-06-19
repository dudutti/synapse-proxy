# 📊 LLM Provider Pricing Reference

> **Usage** : Ce fichier sert de référence pour le super admin dashboard (calcul ROI temps réel, affichage coût/économies).  
> **Format** : prix en USD par million de tokens ($/1M)  
> **Dernière mise à jour** : 2026-06-15  
> **Unité** : Input / Cached Input / Output (en $/1M tokens)

---

## OpenAI

| Modèle | Input | Cached Input | Output | Notes |
|--------|-------|-------------|--------|-------|
| `gpt-5.5` | $5.00 | $0.50 | $30.00 | — |
| `gpt-5.5-pro` | $30.00 | — | $180.00 | Pas de cache natif |
| `gpt-5.4` | $2.50 | $0.25 | $15.00 | — |
| `gpt-5.4-mini` | $0.75 | $0.075 | $4.50 | — |
| `gpt-5.4-nano` | $0.20 | $0.02 | $1.25 | — |
| `gpt-5.4-pro` | $30.00 | — | $180.00 | Pas de cache natif |
| `gpt-4o` | $2.50 | $1.25 | $10.00 | Legacy |
| `gpt-4o-mini` | $0.15 | $0.075 | $0.60 | Legacy |

---

## Anthropic

> ⚠️ **Structure de cache différente d'OpenAI** : Anthropic distingue "Cache Write 5min", "Cache Write 1h" et "Cache Hit". Le cache write coûte PLUS cher que le base input (×1.25 ou ×2). Le cache hit coûte 10% du base input (0.1×). Les writes sont rentabilisés dès le 1er hit (5min) ou le 2e hit (1h).

| Modèle | Input | Cache Write 5m | Cache Write 1h | Cache Hit | Output | Statut |
|--------|-------|----------------|----------------|-----------|--------|--------|
| `claude-fable-5` | $10.00 | $12.50 | $20.00 | $1.00 | $50.00 | Actif |
| `claude-mythos-5` | $10.00 | $12.50 | $20.00 | $1.00 | $50.00 | Limited availability |
| `claude-opus-4.8` | $5.00 | $6.25 | $10.00 | $0.50 | $25.00 | Actif |
| `claude-opus-4.7` | $5.00 | $6.25 | $10.00 | $0.50 | $25.00 | Actif |
| `claude-opus-4.6` | $5.00 | $6.25 | $10.00 | $0.50 | $25.00 | Actif |
| `claude-opus-4.5` | $5.00 | $6.25 | $10.00 | $0.50 | $25.00 | Actif |
| `claude-opus-4.1` | $15.00 | $18.75 | $30.00 | $1.50 | $75.00 | Deprecated |
| `claude-opus-4` | $15.00 | $18.75 | $30.00 | $1.50 | $75.00 | Deprecated |
| `claude-sonnet-4.6` | $3.00 | $3.75 | $6.00 | $0.30 | $15.00 | Actif |
| `claude-sonnet-4.5` | $3.00 | $3.75 | $6.00 | $0.30 | $15.00 | Actif |
| `claude-sonnet-4` | $3.00 | $3.75 | $6.00 | $0.30 | $15.00 | Deprecated |
| `claude-haiku-4.5` | $1.00 | $1.25 | $2.00 | $0.10 | $5.00 | Actif |
| `claude-haiku-3.5` | $0.80 | $1.00 | $1.60 | $0.08 | $4.00 | Retired (Bedrock/Vertex only) |

### 💡 Notes Synapse Proxy × Anthropic

- **Cache hit à 0.1×** = 10% du prix base, identique à OpenAI (~90% de réduction). Le L1 cache natif Anthropic concurrence directement notre L1.
- **Cache write coûte plus cher** que le base input (×1.25 à ×2) → si Synapse Proxy réduit les tokens AVANT le cache write, on économise aussi sur l'écriture cache.
- **Output très élevé** : claude-fable-5 à $50/MTok, claude-opus-4.x à $25/MTok. Synapse Proxy ne réduit pas l'output directement, mais un prompt mieux structuré = réponse plus concise = moins d'output tokens.
- **Client idéal Anthropic** : utilisateurs de claude-opus-4.x et claude-fable-5 avec longs contextes agents/RAG. ROI immédiat.
- **Modèles à éviter comme cible** : claude-haiku-3.5 ($0.80 input) → ROI difficile.

---

## Google (Gemini)

> ⚠️ **Structure unique** : Google applique un **pricing par volume** (≤200K vs >200K tokens d'entrée par requête). Au-delà de 200K tokens, le prix peut **doubler**. Le cache coûte 10% du prix base (×0.1), identique à Anthropic. Certains modèles ont des prix différents "monde" vs "hors monde" (data residency).

### Gemini 2.5 — Texte/Multimodal

| Modèle | Input ≤200K | Input >200K | Cache ≤200K | Cache >200K | Output texte |
|--------|------------|------------|------------|------------|-------------|
| `gemini-2.5-pro` | $1.25 | $2.50 | $0.13 | $0.25 | $10.00 / $15.00 |
| `gemini-2.5-pro` (Computer Use preview) | $1.25 | $2.50 | — | — | $10.00 / $15.00 |
| `gemini-2.5-flash` (texte/img/vidéo) | $0.30 | $0.30 | $0.03 | $0.03 | $2.50 |
| `gemini-2.5-flash` (audio) | $1.00 | $1.00 | $0.10 | $0.10 | $2.50 |
| `gemini-2.5-flash-lite` (texte/img/vidéo) | $0.10 | $0.10 | $0.01 | $0.01 | $0.40 |
| `gemini-2.5-flash-lite` (audio) | $0.30 | $0.30 | $0.03 | $0.03 | $0.40 |

### Gemini 2.5 — Live API (temps réel / voice)

| Type de token | Prix |
|---|---|
| Texte input | $0.50/MTok |
| Audio input | $3.00/MTok |
| Vidéo/Image input | $3.00/MTok |
| Texte output | $2.00/MTok |
| **Audio output** | **$12.00/MTok** |

> Live API = voice-to-voice temps réel. Pas de cache disponible. Audio output très coûteux.

### Gemini 2.5 — Génération d'images

| Modèle | Input texte/image | Output texte | Output image |
|--------|------------------|-------------|-------------|
| `gemini-2.5-flash-image` | $0.30 | $2.50 | **$30.00** |

---

### Gemini 3.x — Texte/Multimodal

| Modèle | Input ≤200K | Input >200K | Cache ≤200K | Cache >200K | Output texte |
|--------|------------|------------|------------|------------|-------------|
| `gemini-3.1-pro` (preview) | $2.00 | $4.00 | $0.20 | $0.40 | $12.00 / $18.00 |
| `gemini-3.5-flash` | $1.50 | $1.50 | $0.15 | $0.15 | $9.00 |
| `gemini-3-flash` (preview) | $0.50 | $0.50 | $0.05 | $0.05 | $3.00 |
| `gemini-3.1-flash-lite` | $0.25 | $0.25 | $0.025 | $0.025 | $1.50 |

> Audio input `gemini-3-flash` : $1.00/MTok (cache $0.10) — `gemini-3.1-flash-lite` audio : $0.50/MTok

### Gemini 3.x — Génération d'images

| Modèle | Input texte/image | Output texte | Output image |
|--------|------------------|-------------|-------------|
| `gemini-3-pro-image` | $2.00 | $12.00 | **$120.00** |
| `gemini-3.1-flash-image` | $0.50 | $3.00 | **$60.00** |

---

### 💡 Notes Synapse Proxy × Google (toutes générations)

- **⚡ Bonus volume tier (critique)** : sur `gemini-2.5-pro`, dépasser 200K tokens **double le prix** ($1.25→$2.50 input, $10→$15 output). Sur `gemini-3.1-pro` idem ($2→$4). Si Synapse Proxy compresse un contexte de 210K → 140K tokens : on passe sous le seuil **et** on réduit les tokens. **Économie potentielle ×3 sur une seule requête.**
- **Cache à ~10%** identique à Anthropic — notre L1/L2 cache (évite 100% du coût) reste plus compétitif que le cache natif Google.
- **Output raisonnement inclus dans le prix output** → attention au calcul ROI, ne pas confondre avec OpenAI.
- **Modèles à éviter comme cible** : `gemini-2.5-flash-lite` ($0.10), `gemini-3.1-flash-lite` ($0.25) → ROI Synapse Proxy difficile.
- **Client idéal Google** : `gemini-2.5-pro` et `gemini-3.1-pro` avec contextes >150K tokens (juste sous le seuil 200K). La compression repousse le franchissement du palier tarifaire.
- **Live API** : Synapse Proxy ne peut pas agir sur le voice-to-voice temps réel (pas de proxy applicable). Hors scope.
- **Image output à $30-120/MTok** : Synapse Proxy n'intervient pas sur l'output image.

---

## Mistral

> ✅ **Avantage Synapse Proxy majeur** : Mistral n'a **pas de cache natif** sur la plupart de ses modèles. Notre L1/L2 cache est donc sans concurrence — 100% d'économie sur les prompts répétés sans que Mistral puisse offrir l'équivalent.

### Modèles texte & raisonnement

| Modèle | API ID | Input | Output | Notes |
|--------|--------|-------|--------|-------|
| `Mistral Medium 3.5` | `mistral-medium-latest` | $1.50 | $7.50 | 128B dense, open weights |
| `Mistral Large 3` | `mistral-large-latest` | $0.50 | $1.50 | Open, multimodal, flagship |
| `Mistral Small 4` | `mistral-small-latest` | $0.10 | $0.30 | Open, Apache 2.0 |
| `Magistral Medium` | `magistral-medium-latest` | $2.00 | $5.00 | Thinking/reasoning, multimodal |
| `Magistral Small` | `magistral-small-latest` | $0.50 | $1.50 | Thinking léger, multimodal |
| `Mixtral 8x7B` | `open-mixtral-8x7b` | $0.70 | $0.70 | SMoE, 12.9B actifs |
| `Mixtral 8x22B` | `open-mixtral-8x22b` | $2.00 | $6.00 | SMoE, 39B actifs |
| `Mistral NeMo` | `open-mistral-nemo` | $0.15 | $0.15 | Code, lightweight |
| `Ministral 3B` | `ministral-3b-latest` | $0.10 | $0.10 | Edge, agentic |
| `Ministral 8B` | `ministral-8b-latest` | $0.15 | $0.15 | Edge, agentic |
| `Ministral 14B` | `ministral-14b-latest` | $0.20 | $0.20 | Edge, agentic |

### Modèles coding

| Modèle | API ID | Input | Output | Notes |
|--------|--------|-------|--------|-------|
| `Devstral 2` | `devstral-medium-latest` | $0.40 | $2.00 | Coding agent, open |
| `Devstral Small 2` | `devstral-small-latest` | $0.10 | $0.30 | Coding léger, multimodal |
| `Codestral` | `codestral-latest` | $0.30 | $0.90 | Fill-in-the-middle, completion |
| `Leanstral` | `labs-leanstral-2603` | **Gratuit** | — | Lean 4, période limitée |

### Modèles voix & audio

| Modèle | API ID | Prix | Notes |
|--------|--------|------|-------|
| `Voxtral TTS` | `voxtral-mini-tts-latest` | $0.016 / 1K chars | Text-to-speech, voice cloning |
| `Voxtral Mini Transcribe 2` | `voxtral-mini-latest` | $0.003 / min audio | Transcription |
| `Voxtral Mini Realtime` | `voxtral-mini-transcribe-realtime` | $0.006 / min audio | Transcription temps réel |
| `Voxtral Small` | `voxtral-small-latest` | $0.004/min (audio) / $0.10/MTok (text) | Output : $0.40/MTok |

### Embedding & modération

| Modèle | API ID | Input | Notes |
|--------|--------|-------|-------|
| `Codestral Embed` | `codestral-embed` | $0.15/MTok | Embeddings code + NL |
| `Mistral Embed` | `mistral-embed` | $0.10/MTok | Embeddings sémantiques |
| `Mistral Moderation` | `mistral-moderation-2603` | $0.10/MTok | Classification contenu |

### OCR

| Modèle | API ID | Prix | Notes |
|--------|--------|------|-------|
| `OCR 3` | `mistral-ocr-latest` | $2 / 1K pages | Extraction documents |
| `OCR 3 (Annotations)` | `mistral-ocr-latest` | $3 / 1K pages | Avec annotations |

### Agent API (outils intégrés)

| Outil | Prix |
|-------|------|
| Web search | $30 / 1K calls |
| Code execution | $30 / 1K calls |
| Image generation | $100 / 1K images |
| Premium news | $50 / 1K calls |
| Libraries / OCR | $3 / 1K pages + $1/MTok indexing |
| Data capture | $0.04/MTok |

### 💡 Notes Synapse Proxy × Mistral

- **✅ Pas de cache natif Mistral** → notre L1 (exact match) et L2 (semantic match) sont sans équivalent. Chaque hit = 100% d'économie là où OpenAI/Anthropic/Google font 90%.
- **`Magistral Medium` à $2/$5** : modèle reasoning compétitif vs GPT-5.4 ($2.50/$15). Fort potentiel car l'output est ×3 moins cher. La compression input est rentable rapidement.
- **`Mistral Large 3` à $0.50/$1.50** : très bon rapport qualité/prix. ROI Synapse Proxy modéré mais la réduction input reste bénéfique sur gros volumes.
- **`Mistral Medium 3.5` à $1.50/$7.50** : output coûteux pour un flagship. Bon candidat pour la compression des prompts agents.
- **Modèles Ministral/Small/NeMo** (<$0.20) → ROI Synapse Proxy difficile à justifier.
- **Voix/OCR/Tools** : hors scope Synapse Proxy (pas de proxy applicable sur ces endpoints).


---

## DeepSeek

> ⚠️ **Structure unique** : DeepSeek distingue "Cache Hit" et "Cache Miss" (pas de write séparé). La remise cache est **98 à 99%** — la plus agressive du marché. Contexte 1M tokens, output max 384K tokens. Compatible **OpenAI format ET Anthropic format** simultanément.

### Modèles V4

| Modèle | API ID | Input (cache miss) | Input (cache hit) | Output | Concurrence max |
|--------|--------|-------------------|------------------|--------|----------------|
| `DeepSeek V4 Flash` | `deepseek-v4-flash` | $0.140 | **$0.0028** | $0.280 | 2 500 req/s |
| `DeepSeek V4 Pro` | `deepseek-v4-pro` | $0.435 | **$0.003625** | $0.870 | 500 req/s |

> **Remise cache** : Flash = **−98%**, Pro = **−99.2%** sur l'input. Le cache hit est quasi gratuit.

### Caractéristiques techniques

| Feature | V4 Flash | V4 Pro |
|---------|----------|--------|
| Context length | 1M tokens | 1M tokens |
| Max output | 384K tokens | 384K tokens |
| Thinking mode | Oui (défaut) + non-thinking | Oui (défaut) + non-thinking |
| JSON Output | ✓ | ✓ |
| Tool Calls | ✓ | ✓ |
| FIM Completion | Non-thinking only | Non-thinking only |
| Format API | OpenAI + **Anthropic** | OpenAI + **Anthropic** |

### 💡 Notes Synapse Proxy × DeepSeek

- **🔥 Cache hit à 98-99%** : la remise la plus agressive du marché, bien au-delà d'OpenAI/Anthropic (90%). Cela signifie que les tokens en cache coûtent presque rien. Notre L1/L2 cache (100% d'économie) reste quand même supérieur mais l'avantage différentiel est réduit vs les autres providers.
- **⚡ Le vrai ROI Synapse Proxy sur DeepSeek** : sur les **cache MISS** ($0.14/$0.435). Si un prompt n'est pas en cache DeepSeek (premier appel, contexte nouveau), Synapse Proxy L3 compresse → moins de tokens facturés au plein tarif.
- **🧠 Contexte 1M tokens = opportunité L3 majeure** : les utilisateurs qui remplissent des contextes longs (RAG massif, agents avec long historique) envoient des centaines de milliers de tokens. À $0.14/MTok (Flash), 500K tokens = $0.07 par requête. Une compression 30% → $0.021 d'économie par appel, rentable sur volumes.
- **Thinking mode activé par défaut** → les prompts agents DeepSeek tendent à être longs et répétitifs. Fort candidat pour L2 (semantic cache) et L3 (compression).
- **Compatible Anthropic format** : les clients qui migrent depuis Claude peuvent pointer vers DeepSeek sans changer de code → Synapse Proxy intercepte les deux formats nativement.
- **Modèle Flash à ne pas négliger** : $0.14 input semble bas, mais à 2500 req/s de concurrence max, les gros utilisateurs envoient des volumes massifs → ROI scale bien.
- **V4 Pro à $0.435** : cible premium, ROI Synapse Proxy solide dès 5K-10K req/mois avec prompts moyens.



---

## Groq

> 🔲 À compléter

| Modèle | Input | Cached Input | Output | Notes |
|--------|-------|-------------|--------|-------|
| `llama-3.3-70b` | — | — | — | À renseigner |
| `llama-3.1-8b` | — | — | — | À renseigner |
| `mixtral-8x7b` | — | — | — | À renseigner |

---

## Together AI

> 🔲 À compléter

| Modèle | Input | Cached Input | Output | Notes |
|--------|-------|-------------|--------|-------|
| `meta-llama/Llama-3-70b` | — | — | — | À renseigner |
| `mistralai/Mixtral-8x22B` | — | — | — | À renseigner |

---

## Perplexity

> 🔲 À compléter

| Modèle | Input | Cached Input | Output | Notes |
|--------|-------|-------------|--------|-------|
| `sonar-pro` | — | — | — | À renseigner |
| `sonar` | — | — | — | À renseigner |

---

## OpenRouter

> OpenRouter est un agrégateur — les prix varient par modèle et incluent une marge OpenRouter.
> Se référer directement à : https://openrouter.ai/models

---

## 📐 Formules de calcul ROI (pour le super admin)

```
// Coût mensuel sans Synapse Proxy
cost_raw = (total_prompt_tokens / 1_000_000) * price_input
         + (total_completion_tokens / 1_000_000) * price_output

// Coût mensuel avec Synapse Proxy
cost_opt = (total_prompt_tokens_opt / 1_000_000) * price_input
         + (total_completion_tokens / 1_000_000) * price_output

// Économie brute
savings = cost_raw - cost_opt

// ROI (net de l'abonnement Synapse Proxy)
roi_net = savings - Synapse Proxy_monthly_fee

// Ratio de compression
compression_ratio = 1 - (total_prompt_tokens_opt / total_prompt_tokens)
```

---

## 💡 Seuils de rentabilité par plan Synapse Proxy

Basé sur une compression moyenne de **30%** et le modèle **gpt-5.5** ($5/1M) :

| Plan Synapse Proxy | Prix/mois | Break-even (req × 500 tokens) | Modèle minimum recommandé |
|---|---|---|---|
| Dev (9€) | $10 | ~6 700 req | gpt-5.4+ ($2.50) |
| Team (29€) | $32 | ~21 000 req | gpt-5.4+ ou gpt-4o |
| Scale (79€) | $87 | ~58 000 req | gpt-5.4+ |
| **Sur gpt-5.5-pro ($30)** | — | — | — |
| Dev (9€) | $10 | ~1 100 req | Rentable très vite |
| Team (29€) | $32 | ~3 600 req | Évident |
| Scale (79€) | $87 | ~9 700 req | ROI ×5 à ×20 |

> **Conclusion** : Sur les modèles nano/mini (<$1/1M), le ROI est difficile.  
> Sur gpt-5.5, gpt-5.5-pro, gpt-5.4-pro, claude-opus → Synapse Proxy est rentable dès les premières centaines de requêtes.
