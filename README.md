<div align="center">
  <img src="https://via.placeholder.com/150x150.png?text=OptiToken" alt="OptiToken Logo" width="120" />
  <h1>OptiToken Proxy</h1>
  <p><strong>Blazing Fast Semantic Cache & API Optimizer for LLMs (OpenAI, Anthropic, Gemini)</strong></p>
  
  <p>
    <a href="https://optitoken.io"><img src="https://img.shields.io/badge/SaaS_Dashboard-Available-emerald?style=for-the-badge" alt="Enterprise SaaS"/></a>
    <img src="https://img.shields.io/badge/Go-1.21-00ADD8?style=for-the-badge&logo=go" alt="Go Version"/>
    <img src="https://img.shields.io/badge/Redis-Stack-DC382D?style=for-the-badge&logo=redis" alt="Redis Stack"/>
  </p>
</div>

---

## ⚡ What is OptiToken?

OptiToken is an ultra-fast, intelligent reverse-proxy written in **Go**. It sits between your application and your LLM provider (OpenAI, Anthropic, DeepSeek, Minimax, etc.) to **reduce latency and significantly slash API costs (up to 10-20% on agentic)**.

Instead of routing every single prompt to the LLM, OptiToken evaluates it using a 4-tier pipeline. If the user asks a question that is **semantically identical** to a previous question (even if phrased slightly differently or in another language), OptiToken serves the response directly from its Redis cache in under `2ms`.

### ✨ Core Features (Open Core)

- 🚀 **High Performance Go Proxy**: Designed to handle thousands of concurrent requests with near-zero overhead.
- 🧠 **Semantic Caching (L2)**: Uses an embedded ONNX vector model (`paraphrase-multilingual-MiniLM-L12-v2`) to detect similar intents across different languages. "How do I loop in Python?" matches "Comment faire une boucle en Python?".
- ⚡ **Exact Matching (L1)**: Instant zero-cost responses for identical prompts using fast xxHash indexing.
- 🗜️ **Payload Optimization (L3)**: If there's a cache miss, OptiToken automatically strips redundant whitespace, prunes stale Chain-of-Thought logs, and minifies your prompt before sending it to the provider.
- 🔄 **Smart Fallback**: Automatically routes to a backup provider/key if the primary API fails or rate-limits you.
- 📊 **Accurate Token Counting**: Integrates the official `tiktoken` BPE tokenizer to estimate your savings perfectly.

### 🤖 Real-World Benchmarks on Autonomous Agents
We benchmarked OptiToken on live Agentic workflows (Hermes) building complete web apps from scratch. 
Because agents loop context indefinitely ("Data Bleeding"), API bills skyrocket. Here is what OptiToken achieved:
- **Orbital Dashboard:** The agent built a complex Next.js dashboard, looping through the codebase structure multiple times. OptiToken intercepted repetitive context and saved **+1,000,000 tokens** in a single session.
- **CollabBoard Kanban (From Scratch):** Building a React & Node.js WebSocket Kanban board entirely from scratch. Since the code was completely new, there were theoretically no "repetitive" queries. Yet, the L3 Payload Compression alone pruned **511,000 tokens** out of a 6M token session (an ~8-10% net reduction on a worst-case scenario!).

---

## 🏗️ Architecture

OptiToken operates using a localized **Redis Stack** with Vector Search (`RedisSearch`) enabled. 

1. **Request Received**: The Go proxy intercepts standard `/v1/chat/completions` API calls.
2. **L1 Cache (Hash)**: Checks for an exact payload match.
3. **L2 Cache (Semantic)**: Extracts the text, runs it through the local Python/ONNX embedding service, and searches Redis for vectors with a Cosine Distance below the configured tolerance (default 0.15).
4. **L3 Compression**: If missed, compresses the prompt and forwards it to the LLM.
5. **Streaming**: Responses stream back to the client transparently while asynchronously saving into Redis.

---

## 🚀 Getting Started (Self-Hosted)

You can run the OptiToken proxy locally using Docker Compose.

```bash
git clone https://github.com/your-org/optitoken.git
cd optitoken

# Start the Redis Stack, ONNX Embedder, and Go Proxy
docker compose up -d
```

Your proxy is now running on `http://localhost:8080`.

Point your OpenAI SDK to the proxy:

```python
from openai import OpenAI

client = OpenAI(
    api_key="your_real_openai_key",
    base_url="http://localhost:8080/v1"
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello world!"}]
)
```

---

## 💎 OptiToken Cloud / Enterprise SaaS

While the proxy engine is open-source, managing virtual keys, tracking exact financial savings across your organization, and visually debugging semantic hits requires robust infrastructure.

**OptiToken Enterprise** is our fully hosted SaaS platform designed for production teams.

### Why use the SaaS?

- 🔑 **Virtual Key Management**: Issue revocable `sk-opti-...` keys to your team without exposing your real OpenAI API keys.
- 📈 **Real-Time Analytics**: Track exactly how much money you've saved per day, per model, and per user.
- 🛠️ **Side-by-Side Playground**: Visually A/B test your prompts against the Direct API vs OptiToken cache to see the latency drop in real-time.
- 🔁 **Advanced Fallbacks**: Configure complex cascading fallback trees across 5+ different AI providers (Anthropic -> OpenAI -> DeepSeek).
- ☁️ **Fully Hosted Proxy**: No Docker, no Redis scaling, no ONNX RAM management. Change your `base_url` to our edge network and start saving instantly.

👉 **[Sign up for the Waitlist at -soon**

---

## 🤝 Contributing

We welcome contributions! Please see our `CONTRIBUTING.md` for details on how to set up the Go and Next.js environments for local development.

## 📄 License

The OptiToken Proxy is open-sourced under the MIT License. The Enterprise Dashboard is proprietary.
