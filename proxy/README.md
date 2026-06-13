# OptiToken Proxy 🚀

OptiToken is a high-performance, open-source proxy written in Go that acts as a drop-in replacement for the OpenAI/Anthropic API. It intercepts your LLM calls and applies a 3-tier caching system to drastically reduce your API bills (by 10% to 100% depending on the workload).

## ✨ Features
1. **L1 Cache (Exact Match):** Returns cached responses for identical prompts in <2ms (0$ cost).
2. **L2 Cache (Semantic Vector Match):** Uses a local ONNX embedding model to detect intent. If a user asks "How to reset password?" and another asks "Forgot password, what do I do?", OptiToken returns the cached response.
3. **L3 Cache (Payload Compression):** Lossless minification of JSON payloads and aggressive pruning of stale Chain-of-Thought logs before forwarding the request to the upstream provider.
4. **Multi-Tenant Isolation:** Automatically segments the semantic cache by end-user using the `user` parameter in the OpenAI payload. Prevents data bleeding between clients.

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
