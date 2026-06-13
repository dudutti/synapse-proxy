# OptiToken User Guide

Welcome to OptiToken, your AI API cost-saving proxy.

This guide covers both the **Open-Source Local Proxy** (for self-hosting) and the **OptiToken SaaS Dashboard** (for Enterprise/Team management).

---

## 🏗️ 1. Deploying the Open-Source Proxy Locally

If you want to run OptiToken entirely on your own infrastructure, you can deploy the core proxy engine using Docker. The proxy will handle caching and compression locally.

### Prerequisites
- Docker & Docker Compose
- An OpenAI / Anthropic / Minimax API key

### Deployment Steps
1. **Clone the repository:**
   ```bash
   git clone https://github.com/dudutti/Optitoken.git
   cd Optitoken/proxy
   ```

2. **Start the services:**
   The Open Source package includes three containers:
   - **Redis Stack**: For the L1 (Exact) and L2 (Semantic) vector cache.
   - **ONNX Embedder**: A lightweight Python service running `paraphrase-multilingual-MiniLM-L12-v2` locally to calculate semantic vectors.
   - **Go Proxy**: The ultra-fast core engine.

   ```bash
   docker-compose up -d --build
   ```

3. **Configure your App:**
   The proxy now listens on `http://localhost:8080/v1`.
   Because you are running the Open Source version without the SaaS backend, the proxy uses a default fallback key mechanism. Just pass your real provider API key through the standard `Authorization` header, and change the Base URL in your application.

   ```python
   from openai import OpenAI
   
   client = OpenAI(
       base_url="http://localhost:8080/v1",
       api_key="your-real-openai-key" 
   )
   
   response = client.chat.completions.create(
       model="gpt-4o",
       messages=[{"role": "user", "content": "Hello!"}]
   )
   ```

---

## ☁️ 2. Using the OptiToken SaaS Dashboard

For teams that don't want to manage Docker containers or want advanced tracking (live telemetry, financial ROI calculation, and visual A/B testing), we offer the **OptiToken SaaS Dashboard**.

*(Available at [OptiToken.io](https://optitoken.io))*

### Step 1: Account & Key Management
1. **Create an account:** Navigate to `/signup`.
2. **Secure your Provider Keys:** Go to **Settings** (`/settings`). Under "Generate Virtual OptiToken Key", select your Provider (e.g., OpenAI, Minimax) and paste your real API Key (`sk-...`). 
3. **Get your Virtual Key:** Click **Generate**. OptiToken securely encrypts your real key and gives you a safe Virtual Key (`sk-opti-...`).
4. **Integration:** Use this Virtual Key in your code. Our edge proxy handles the routing!

### Step 2: The Playground & Telemetry
1. Navigate to `/playground` to test queries visually. The UI will instantly show if a query triggered an **API Call** (Full price), an **L1 Hit** (0ms, 0$), or an **L2 Hit** (Semantic Match).
2. The **Live Telemetry** dashboard automatically converts your saved tokens into real Dollar (`$`) savings based on the model's pricing.

### Step 3: Configuring Semantic Cache (L2) Tolerance
In the SaaS Settings, you can dynamically adjust the **Tolerance (Sensitivity)** of the Semantic Cache for your Virtual Keys:
- **Strict (0.05)**: Only near-identical prompts will hit the cache. High accuracy, lower savings.
- **Loose (0.30)**: Broadly similar prompts hit the cache. Maximum savings, but nuance might be lost.
- **Default (0.15)**: The sweet spot for support bots and conversational AI.

### Step 4: The Benchmark Mode (LLM Judge)
If you want to ensure the Semantic Cache isn't returning inaccurate responses, use the **Benchmark Tab**. OptiToken will run your query twice (once via Cache, once via Direct API) and an independent LLM Judge will score the cache's reliability!
