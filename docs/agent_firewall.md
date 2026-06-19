# Agent Firewall

The Agent Firewall is the per-virtual-key safety net that sits between
the SDK client (Hermes, Claude Code, Cursor, OpenAI Python, curl, …) and
the upstream LLM provider. It groups four independent mechanisms:

| Mechanism | Trigger | Response | Default |
|---|---|---|---|
| **Loop kill switch** | Same full request body 3× in 60s | HTTP 200 + Self-Correction Assistant Message Warning | OFF per key |
| **Tool fingerprint (soft loop)** | Same `(tool, args)` 4× in 30s **AND** cache miss on a non-read-only tool | HTTP 200 + Self-Correction Assistant Message Warning | OFF per key |
| **Tool filter** | Tool name not in `AllowedTools` while `BlockUnknownTools` is on | HTTP 400 + `Agent Firewall blocked unauthorized tool call: <name>` | OFF per key |
| **Session token cap** | Cumulative prompt tokens per `session_id` exceeds the configured limit | HTTP 400 + `Agent Firewall: Session token limit exceeded` | OFF per key |
| **PII redaction** | (always on when enabled) | emails stripped from the request body before upstream | OFF per key |

All toggles are configured per virtual key, in **Settings → API Keys →
Agent Firewall**. Each toggle maps to a Redis hash field on
`synapse:keys:<vk>` (see `proxy/internal/services/auth.go`).

---

## Tool fingerprint (soft loop detect)

The fingerprint is the most nuanced of the four. It exists because some
agents (notably Hermes and Claude Code) legitimately re-invoke the same
tool with the same arguments several times in a row to iterate on a plan,
and we don't want to punish that. The strategy is layered:

### Layer 1 — Read-only tools are exempt

Tools that are safe to re-serve from cache indefinitely are skipped by
the fingerprint entirely. They are never counted, never blocked. The
proxy maintains two lists:

- **Explicit allowlist** (`readOnlyToolNames` in
  `proxy/optiagent/tool_fingerprint.go`): `todo`, `todos`, `todo_write`,
  `plan`, `think`, `reflect`, `reason`, `read_file`, `list_files`,
  `get_status`, `search_web`, …
- **Prefix matcher** (`readOnlyToolPrefixes`): `read_`, `list_`,
  `get_`, `find_`, `search_`, `fetch_`, `lookup_`, `query_`, `check_`,
  `inspect_`. Catches the long tail without enumeration.

So if the agent calls `todo` 50 times in 30s, we just serve the cache
each time. Zero upstream tokens. Zero false positives.

### Layer 2 — Stateful tools: cache-first

For stateful tools (`write_file`, `send_email`, `shell_exec`, …) the
fingerprint counts identical retries. But the actual 429 only fires if
ALL of these are true at the same time:

1. The fingerprint counter for this `(vk, tool, args)` has reached
   `FingerprintThreshold` (default **4**) within `FingerprintWindowSecs`
   (default **30s**).
2. The cache check (L1 exact + L2 semantic + LOOP reuse) **missed** —
   there is nothing cached to re-serve.
3. The tool is not in the read-only allowlist.

If the cache hit, we serve the cached payload and the agent moves on,
even if the counter is at 50.

### Layer 3 — Kill switch (hard cap)

Independent of the fingerprint: the loop detector in
`proxy/optiagent/loop_detect.go` hashes the **full request body** and,
on the 3rd identical hash within 60s, intercepts the loop and returns a mock
chat completion message (`HTTP 200`) containing a descriptive self-correction warning.

The kill switch fires *before* the cache check and is unaffected by the
read-only exemption. The kill switch is OFF by default per key (`KillSwitch` in
the Agent Firewall modal).

### Putting it together: the agent's experience

For a Hermes-style agent that hammers `todo` to manage its plan:

```
Tour 1:  todo({list})                → cache miss  → upstream        → cache stored
Tour 2:  todo({list})   3s later      → L1 cache hit → 200 + cached
Tour 3:  todo({list})   5s later      → L1 cache hit → 200 + cached
...
Tour 50: todo({list})                 → L1 cache hit → 200 + cached
```

For an agent that retries a stateful tool because something is wrong:

```
Tour 1:  write_file(config.json, "X")  → cache miss  → upstream → cache stored
Tour 2:  write_file(config.json, "X")  → cache hit (<60s) → 200 + cached
Tour 3:  write_file(config.json, "X")  → cache hit (<60s) → 200 + cached
Tour 4:  write_file(config.json, "X")  → cache age>60s, count=4, miss
                                          → HTTP 200 + Assistant self-correction warning
                                          → agent reads warning and changes strategies
```

---

## Response headers (for debugging)

When the fingerprint is enabled and observes a tuple, the proxy sets:

| Header | Meaning |
|---|---|
| `X-Synapse-Fingerprint-Count` | The current retry count for the worst tuple in the body |
| `X-Synapse-Fingerprint-Tool` | The function name that tripped |

When the kill switch fires or the soft loop blocks (cache miss + count ≥ 4), the proxy intercepts the execution and returns a mock OpenAI-compatible Chat Completion response (`HTTP 200`) with the assistant warning:

```json
{
  "choices": [
    {
      "finish_reason": "stop",
      "index": 0,
      "message": {
        "content": "Attention : Vous venez de répéter l'outil <tool> avec les mêmes arguments. Veuillez vérifier vos actions précédentes ou changer de stratégie pour éviter une boucle infinie.",
        "role": "assistant"
      }
    }
  ],
  "created": 1781897465,
  "id": "chatcmpl-selfcorrect",
  "model": "MiniMax-M3",
  "object": "chat.completion"
}
```

This injects the warning directly into the agent's chat history, enabling it to auto-correct and try a different task plan without crashing the framework.

---

## Discovered tools (auto-allow by default)

The proxy auto-records every tool name it sees into the Redis set
`synapse:discovered_tools:<vk>` (TTL 30 days). The dashboard reads
that set and renders a checkable list in **Settings → API Keys →
Agent Firewall → Discovered Tools**.

- Tools that are **checked** are allowed (the default).
- Tools that are **unchecked** are added to the Redis set
  `synapse:denied_tools:<vk>`. The proxy consults that set on every
  request and returns HTTP 403 if the agent tries to call one.

The list is the inverse of `AllowedTools`: instead of enumerating every
tool the agent might call (a maintenance burden), we let the proxy
observe them and let the operator only act on the ones they want to
block.

The endpoint is exposed at `/api/admin/discovered-tools?vk=<vk>` for the
dashboard and `/v1/keys/tools?vk=<vk>` for direct operator access.

---

## Configuration reference

| Redis field | Dashboard field | Default | Effect when on |
|---|---|---|---|
| `kill_switch` | Kill Switch | `false` | Mock HTTP 200 warning on full-body repeat 3× in 60s |
| `fingerprint_loop_detect` | Soft Loop Detect | `false` | Mock HTTP 200 warning on `(tool,args)` 4× in 30s + cache miss, exempts read-only tools |
| `block_unknown_tools` | Block Tool Calls | `false` | 400 on tool outside `allowed_tools` |
| `allowed_tools` | Allowed Tools | `""` | Comma-separated whitelist |
| `session_token_limit` | Session Token Cap | `0` (off) | 400 on cumulative prompt tokens > limit |
| `redact_pii` | Redact PII | `false` | Strips email-shaped substrings before upstream |
| `tool_ttls` | Tool Cache TTLs | `"{}"` | JSON string defining granular cache TTLs per tool name (e.g. `{"write_file": 300, "web_search": 0}`) |

All fields are read on every request via `HGETALL synapse:keys:<vk>`
and decoded in `proxy/internal/services/auth.go`.

---

## Tool-Specific Cache TTLs

The firewall supports configuring granular cache TTL policies per tool name:
- **Disabling caching**: Set a TTL of `0` for any tool name (e.g. `{"write_file": 0}`). The proxy will completely skip caching for requests containing this tool call, forcing them upstream.
- **Custom durations**: Set a TTL in seconds (e.g. `{"web_search": 3600}`) to enforce a specific age limit on cache entries containing this tool call.
- **Fallback**: If no tool-specific override is found, the proxy defaults to whitelisting read-only tools (infinite cache lifespan) and limiting stateful tools to `60` seconds.

Operators can configure these TTLs per virtual key directly in the Dashboard under **Firewall Modal → Tool Cache TTLs**. Changes are written to Postgres and synced to the `tool_ttls` Redis hash field.