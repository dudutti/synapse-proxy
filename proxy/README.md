# OptiToken Proxy 🚀

OptiToken is a high-performance, open-source proxy written in Go that acts as a drop-in replacement for the OpenAI/Anthropic API. It intercepts your LLM calls and applies a **4-tier caching system** to drastically reduce your API bills (by 10% to 100% depending on the workload).

## ✨ Features
1. **L0 Cache (In-flight Deduplication):** Two identical requests arriving at the same time are collapsed into one — the leader runs the full pipeline, the follower blocks and waits up to 30s for the result. 0$ cost on request bursts. Telemetry tags followers as `L0 Coalesced`.
2. **L1 Cache (Exact Match):** Returns cached responses for identical prompts in <2ms (0$ cost).
3. **L2 Cache (Semantic Vector Match):** Uses a local ONNX embedding model to detect intent. If a user asks "How to reset password?" and another asks "Forgot password, what do I do?", OptiToken returns the cached response. Auto-disabled during multi-turn agentic trajectories (Record Sessions, tool-call loops) to avoid corrupting context with stale-turn responses.
4. **L3 Cache (Payload Compression):** Prunes stale Chain-of-Thought logs, deletes `reasoning_content` from old assistant turns, and truncates huge tool outputs (>200 chars) before forwarding. Only applied when the compression actually shrinks the payload in both bytes and tokens — never inflates.
5. **Multi-Tenant Isolation:** Automatically segments the semantic cache by end-user using the `user` parameter in the OpenAI payload. Prevents data bleeding between clients.

**Security:** Real provider API keys are encrypted with **AES-256-GCM** (authenticated encryption) before being written to both PostgreSQL and Redis, using a shared `ENCRYPTION_KEY` (32-byte hex) configured via `.env`. They are only decrypted in-memory at request time.

**Pricing accuracy:** Model pricing is loaded from the `ProviderModel` PostgreSQL table by a background syncer refreshed every hour. No hardcoded fallback for known providers — unknown models fall back to a generic $1/MTok for safety.

## 📊 Real-World Benchmarks on Autonomous Agents
We benchmarked OptiToken on live Agentic workflows (Hermes) building complete web apps from scratch. 
Because agents loop context indefinitely ("Data Bleeding"), API bills skyrocket. Here is what OptiToken achieved:

### 1. Orbital Dashboard (Architecture & Looping)
- **The Task:** The agent built a complex Next.js dashboard, looping through the codebase structure multiple times.
- **The Result:** OptiToken intercepted repetitive context and saved **+1,000,000 tokens** in a single session!

### 2. CollabBoard Kanban (100% From Scratch)
- **The Task:** Building a React & Node.js WebSocket Kanban board entirely from scratch. Since the code was completely new, there were theoretically no "repetitive" questions.
- **The Result:** The L3 Payload Compression alone pruned **511,000 tokens** out of a 6M token session. That's a ~10% net reduction on a worst-case scenario!

## 🚀 Quick Start (Docker)

1. Clone this repository.
2. Ensure you have Docker and Docker Compose installed.
3. Run the following command:
```bash
docker-compose up -d --build proxy
```
*(The docker-compose file includes the Proxy, Redis, and the local ONNX embedding container).*

## 🔌 Integration
OptiToken is a 100% transparent proxy. You don't need to change your application code. Just change your API Base URL to point to OptiToken.

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1", 
    api_key="sk-opti-YOUR_VIRTUAL_KEY" 
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello world!"}]
)
```

## 🛠 Architecture
- **Go 1.21** (Proxy core, high concurrency, SSE streaming support)
- **Redis + RediSearch (VSS)** (Fast storage and semantic K-Nearest Neighbors search)
- **Python ONNX Embedder** (Local lightweight embedding model `paraphrase-multilingual-MiniLM-L12-v2`)

## 📄 License
MIT License.
