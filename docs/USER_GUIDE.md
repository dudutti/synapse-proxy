# OptiToken User Guide

Welcome to OptiToken, your AI API cost-saving proxy.

## 1. Creating an Account
1. Navigate to `/signup` to create a new account.
2. An email with a verification link will be sent to your inbox.
   - *Note for developers*: If SMTP is not configured, check the terminal logs for an `Ethereal Email Preview URL` to click the verification link.
3. Click the verification link to activate your account.
4. Log in at `/login`.

## 2. Managing Your API Keys
To use OptiToken, you need to store your real LLM API keys securely.
1. Go to **Settings** (`/settings`).
2. In the "Generate Virtual OptiToken Key" section, select your Provider (e.g., OpenAI, Minimax).
3. Paste your real API Key (`sk-...`).
4. Click **Generate**. OptiToken will encrypt your real key at rest in the PostgreSQL database and provide you with a Virtual Key (`sk-opti-...`).
5. Use this Virtual Key in your applications!

## 3. Configuring the Semantic Cache (L2)
In the Settings table, you can adjust the **Tolerance (Sensitivity)** of the Semantic Cache for each key.
- **Strict (0.05)**: The proxy will only return a cached response if the prompt is nearly identical.
- **Loose (0.30)**: The proxy will return a cached response for broadly similar prompts, maximizing cost savings but potentially returning slightly inaccurate answers for nuanced questions.
- **Default (0.15)** is recommended for a great balance.

## 4. The Playground
You can test your Virtual Keys and cache rules without writing code:
1. Navigate to `/playground`.
2. Select one of your active Virtual Keys.
3. Send a message.
4. The playground will tell you whether the response was processed by the LLM (API Call) or if it hit the cache (L1 or L2 Cache Hit).

## 5. Billing & Subscription
1. Go to **Settings** -> **Subscription & Billing**.
2. OptiToken offers three plans: Hobby (Free), Pro, and Enterprise.
3. Upgrading to Pro increases your token optimization limit and unlocks priority support.

## 6. How to Use OptiToken in Your Code
Simply replace your provider's base URL with your OptiToken Proxy URL, and use your `sk-opti-...` key.

**Python Example:**
```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="sk-opti-..."
)

response = client.chat.completions.create(
    model="gpt-3.5-turbo",
    messages=[{"role": "user", "content": "Hello!"}]
)
```
