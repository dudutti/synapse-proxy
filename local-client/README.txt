======================================================================
              Synapse Proxy Local Client - Quick Start Guide
======================================================================

Synapse Proxy is a local LLM caching and optimization proxy. It starts
a local proxy at port 8080 and a control dashboard at port 4321.

----------------------------------------------------------------------
0. WHAT'S NEW
----------------------------------------------------------------------
This build includes the full L3 byte-preserving compression pipeline:

  - Tool output truncation (content > 200 chars trimmed in place)
  - Thinking block stripping (<thinking>...</thinking>, <thought>...</thought>)
  - Repeated tool result blanking (3rd+ consecutive same-name tool)
  - Todo-list carve-out (anything matching status:"pending" or
    "todos":["..."]:["...]" is preserved verbatim so multi-turn
    agents keep their plan visible)

Plus optional OpenAI↔Anthropic translation for providers that
support /v1/messages (Anthropic, OpenAI, MiniMax, DeepSeek,
Bedrock, Vertex). Add ?backend=anthropic-compatible in the
X-Synapse-Provider header to enable. Saves 30-99% on cache_read
when the prefix is byte-stable across turns.

Local L1/L2/L3 cache uses SQLite. L1 keys on SHA-256 of the
post-L3 payload (byte-stable, idempotent). L2 and L3 use
Jaccard similarity on the system prompt and last user message.

----------------------------------------------------------------------
1. WINDOWS DEFENDER WARNING (False Positive)
----------------------------------------------------------------------
Because this executable is compiled directly from source and is not
digitally signed with an expensive corporate certificate, Windows Defender
will block it upon download.

To allow and run the application:
1. Open Windows Security (Sécurité Windows).
2. Go to "Virus & threat protection" (Protection contre les virus et menaces).
3. Click on "Protection history" (Historique des protections).
4. Locate the blocked threat for "synapse-local.exe".
5. Click on "Actions" -> select "Allow on device" (Autoriser sur l'appareil)
   or "Restore" (Restaurer).

----------------------------------------------------------------------
2. HOW TO LAUNCH
----------------------------------------------------------------------
Simply double-click "synapse-local.exe" or run it from the console:
  .\synapse-local.exe

It will automatically initialize its local database "synapse_local.db"
in the same folder. No installer is required.

----------------------------------------------------------------------
3. CONFIGURATION & ACCESS
----------------------------------------------------------------------
- Dashboard Access: Open http://localhost:4321 in your browser.
- Proxy Endpoint: Configure your tools to point to:
  http://localhost:8080/v1

To activate your premium quotas, copy your license key from the
cloud settings page and paste it into the "Local Client License"
field on your local settings tab (http://localhost:4321/settings).

----------------------------------------------------------------------
4. PROVIDER-SPECIFIC HEADERS
----------------------------------------------------------------------
The proxy selects its upstream and translates the payload based
on the X-Synapse-Provider header. Common values:

  X-Synapse-Provider: ollama        -> http://localhost:11434/v1/chat/completions
  X-Synapse-Provider: lmstudio      -> http://localhost:1234/v1/chat/completions
  X-Synapse-Provider: anthropic     -> https://api.anthropic.com/v1/messages
  X-Synapse-Provider: minimax       -> https://api.minimax.io/v1/chat/completions
  X-Synapse-Provider: minimax-anthropic
                                    -> https://api.minimax.io/anthropic/v1/messages
                                      (auto-translates payload, enables cache_read)
  X-Synapse-Provider: deepseek      -> https://api.deepseek.com/chat/completions
  X-Synapse-Provider: openai       -> https://api.openai.com/v1/chat/completions

If X-Synapse-Provider is missing, defaults to "openai".

----------------------------------------------------------------------
5. AUTHENTICATION HEADERS
----------------------------------------------------------------------
The proxy uses the standard Authorization: Bearer <key> header
on the wire to the SaaS endpoint. For Anthropic-compatible
endpoints, the proxy automatically rewrites the header to
x-api-key: <key> with the anthropic-version: 2023-06-01
header added.

----------------------------------------------------------------------
6. CACHE BEHAVIOR
----------------------------------------------------------------------
  L1  : exact byte-match (SHA-256 of post-L3 payload)
  L2  : Jaccard similarity >= 0.85 on system prompt
  L3  : Jaccard similarity >= 0.70 on system prompt or last user

The cache is stored locally in synapse_local.db (SQLite). The
dashboard displays the hit/miss ratio and per-cache-level
savings under the "Analytic" tab.

----------------------------------------------------------------------
7. TESTING THE BUILD
----------------------------------------------------------------------
Run the unit tests for the compression pipeline:

  cd local-client
  go test ./internal/compress/

Expected: all tests pass with the message "ok".

======================================================================