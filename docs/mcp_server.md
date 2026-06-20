# Model Context Protocol (MCP) Server

Synapse Proxy acts as a powerful Model Context Protocol (MCP) server, allowing developers to integrate proxy configuration, metrics, analytics, and loop control tools directly into their MCP-compatible IDEs (such as Cursor, Claude Code, Continue, or Windsurf).

Since Synapse Proxy is fully open source, **all 14 tools are completely free to use**. For tools that interact with the dashboard (such as creating virtual keys, managing alerts, or recording sessions), they connect directly to your self-hosted Next.js dashboard instance (located in `./dashboard`) when running in self-hosted mode.

---

## 🚀 Execution Modes

The MCP server can be run in two modes:

### 1. Stdio Mode (CLI & Local IDEs)
Stdio transport is ideal for local IDE integrations (like Cursor running on the same machine). Each client connection spawns a new proxy process and communicates over stdin/stdout.

```bash
# Run proxy as an MCP server using stdio transport
./synapse-proxy --mcp --mcp-tier=full
```

### 2. HTTP Mode (Remote & SaaS)
SSE (Server-Sent Events) HTTP transport is ideal for long-lived servers running behind reverse proxies (like Caddy or Nginx).

```bash
# Run proxy as an MCP server over HTTP (default port 8081)
./synapse-proxy --mcp-http --mcp-http-port=8081 --mcp-tier=full --dashboard-url=http://dashboard:3000
```

---

## 🛠️ The 14 Available Tools

The MCP server advertises 14 tools divided into three categories: **Gateway Operations**, **Key & Billing Management**, and **Observability & Alerting**.

### A. Gateway Operations

#### 1. `synapse_chat_completions`
* **Description**: Send a chat completion through the Synapse Proxy. Applies L1 exact cache, L2 semantic cache, and L3 prefix-preserving compression before forwarding upstream.
* **Arguments**:
  * `messages` (Array, Required): List of chat messages (with `role` and `content`).
  * `model` (String, Required): Model string to target (e.g. `gpt-4o-mini`).
  * `temperature` (Number): Sampling temperature.
  * `max_tokens` (Integer): Maximum completion tokens.
  * `stop` (Array of Strings): Stop sequences.
* **Returns**: The OpenAI-shaped response plus a `synapse_enrichment` object with `cache_level`, `tokens_saved`, and `cost_saved_usd`.

#### 2. `synapse_list_models`
* **Description**: Retrieve the list of active models the proxy knows how to serve.
* **Arguments**: None.
* **Returns**: Array of models containing the provider name, model name, and cost-per-million tokens for both prompt and completion.

---

### B. Observability & Session Replay

#### 3. `synapse_cache_stats`
* **Description**: Get overall cache efficiency stats (total requests, total tokens saved, total cost saved, and per-level hit rate breakdown).
* **Arguments**:
  * `days` (Integer, Default: 7): Timeline window for query.
* **Returns**: JSON object with cache distribution counters for L1, L2, L3, L0, and misses.

#### 4. `synapse_savings_summary`
* **Description**: Cost-savings report aggregated by token class (InputFresh, CacheRead, CacheCreation, Output) and provider.
* **Arguments**:
  * `days` (Integer, Default: 30): Range of logs to scan.
* **Returns**: Sum of cost saved in USD and total prompt/completion tokens processed.

#### 5. `synapse_start_session`
* **Description**: Starts a live traffic recording session on the dashboard.
* **Arguments**:
  * `label` (String): Friendly label for the session.
  * `group_by` (String, default: "agent"): Grouping identifier (agent, model, or session).
* **Returns**: A unique `session_id` which tags subsequent requests.

#### 6. `synapse_stop_session`
* **Description**: Stops a running session recording and stops tagging incoming traffic.
* **Arguments**:
  * `session_id` (String, Required): The session to stop.
* **Returns**: Success confirmation.

#### 7. `synapse_list_sessions`
* **Description**: List the most recent recording sessions and their telemetry summaries.
* **Arguments**:
  * `limit` (Integer, default: 20): Maximum sessions to return.
* **Returns**: List of sessions with their creation times and group tags.

#### 8. `synapse_export_logs`
* **Description**: Export request log telemetry as raw CSV or JSONL.
* **Arguments**:
  * `format` (String, default: "jsonl"): Format (`csv` or `jsonl`).
  * `since` (String): ISO 8601 start date.
  * `until` (String): ISO 8601 end date.
* **Returns**: Text stream of log rows.

---

### C. Key & Firewall Configuration

#### 9. `synapse_list_virtual_keys`
* **Description**: List all active virtual keys (`sk-opti-...`) configured in the system.
* **Arguments**: None.
* **Returns**: Array of key properties (id, provider, budget, current usage, creation timestamp).

#### 10. `synapse_create_virtual_key`
* **Description**: Create a new API key with a designated provider and budget limits.
* **Arguments**:
  * `label` (String, Required): Key description.
  * `provider` (String, Required): The target LLM provider (openai, anthropic, minimax, etc.).
  * `model` (String): Default model.
  * `monthly_budget` (Number, Required): Budget cap in USD.
* **Returns**: Plaintext virtual API key (shown only once).

#### 11. `synapse_get_quotas`
* **Description**: Inspect monthly budget and current token consumption across all virtual keys.
* **Arguments**: None.
* **Returns**: Usage ratio vs budget per key.

#### 12. `synapse_run_benchmark`
* **Description**: Run an automated side-by-side A/B benchmark test between two models using a judge LLM to score semantic similarity.
* **Arguments**:
  * `models` (Array of Strings, Required): Exactly 2 model names to compare.
  * `prompt` (String, Required): Query prompt.
  * `judge_model` (String): Model to act as judge (defaults to `gpt-4o-mini`).
  * `runs` (Integer): Number of test runs to average.
* **Returns**: Semantic similarity scores (0-100), latency comparison, and judge feedback.

#### 13. `synapse_list_alerts`
* **Description**: List active firewall alerts and triggered event history.
* **Arguments**:
  * `triggered_only` (Boolean, default: false): Filter active triggers.
* **Returns**: List of alert rule definitions.

#### 14. `synapse_set_alert_rule`
* **Description**: Configure or edit an alert threshold (e.g. notify via Email/Slack on monthly budget usage, high upstream error rate, or low cache hit rate).
* **Arguments**:
  * `rule_id` (String): ID to update (omit to create a new one).
  * `name` (String, Required): Rule name.
  * `metric` (String, Required): Metric kind (`monthly_spend_usd`, `cache_hit_rate`, `upstream_error_rate`).
  * `threshold` (Number, Required): Alert limit value.
  * `channel` (String, Required): Notification channel (`email`, `slack`, `webhook`).
  * `target` (String, Required): Destination hook URL or email address.
  * `enabled` (Boolean, default: true): Enable status.
* **Returns**: Created or updated rule details.

---

## 🔌 IDE Integration Example (Cursor)

Add the following config in Cursor under **Settings → Features → MCP**:

* **Name**: `synapse-proxy`
* **Type**: `command`
* **Command**: `C:\path\to\synapse-proxy.exe --mcp --mcp-tier=full` (On Windows, specify the full path to your built proxy binary)
* **Environment Variables**:
  * `REDIS_ADDR`: `localhost:6379`
  * `DATABASE_URL`: `postgresql://optitoken_admin:password@localhost:5432/optitoken_db`
  * `ENCRYPTION_KEY`: `your_32_byte_hex_encryption_key`
  * `DEFAULT_VIRTUAL_KEY`: `sk-opti-your_active_key`
