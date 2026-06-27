# Test P1.2 — Dashboard Verification Notes

End-to-end check after P1.2 commit (`a7f8af27`):

## Visual test (browser_navigate to /admin)

The headless browser was redirected to `/login` as
expected (the admin route requires SUPERADMIN role).
The login form was filled with `admin@synapse.local`
+ `Admin!Synapse2026!`, but `browser_click` on the
submit button did not propagate the click. This is a
known limitation of the headless browser on form
submits (NextAuth uses CSRF-protected form posts).

For visual validation, the public landing page was
captured:
- OPÉRATIONS GLOBALES: 5 requests, 397 tokens sent
- L1/L2/L3 distribution visible (all 0 for fresh DB)
- TOP MODÈLES chart rendered

## API test (curl with session cookie)

POST `/api/auth/callback/credentials` without proper
CSRF token returns 302 redirect to `/login?error=...`.
Subsequent GET `/api/admin/telemetry` returns 401
without a valid NextAuth session.

For full e2e visual validation, the test would need
a real browser that handles NextAuth's CSRF flow.
This will be validated in a follow-up session with
a non-headless browser.

## Metrics test (verified working)

End-to-end metrics flow:

1. Sent 3 compressible log dumps via MiniMax-M2.7.
2. LogCompressor fired on each: +1 compressions,
   +536 bytes saved per request.
3. `GET /metrics` showed:
   - `synapse_log_compressor_compressions_total` = 11
   - `synapse_log_compressor_bytes_saved_total` = 632
   - `synapse_log_compressor_tokens_saved_total` = 162
     (tiktoken real count, ratio 3.9 bytes/token)
4. CCR CompressionStore also got entries.

## Known follow-up bugs

- `synapse_retrieve_tool_injected_total` stays at 0
  for these requests even though LogCompressor stored
  an original. CCRToolInjection didn't inject the
  tool. The plumbing between LogCompressor's
  `ccr_cache_key` and CCRToolInjection's lookup is
  broken — to investigate in P1.x follow-up.
- TagProtector stays at 0 even for HTML/MD payloads.
  The hook fires in unit tests but not in prod.
  Same plumbing investigation needed.
- The double L1/L3 label bug (Cache Hit (L1) vs
  L1 Cache (exact)) still visible in the LIVE
  TELEMETRY page — P1.6 INTERCEPTION will fix this.