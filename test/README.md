# OptiToken Test Suite

This directory contains reproducible tests and real benchmark
data for the OptiToken proxy. Tests are organized by feature.

## Subdirectories

- [`ab_benchmark_2026_06_18/`](ab_benchmark_2026_06_18/) — A/B
  benchmark validating Phase 2 of the cache-preserving L3
  architecture. Includes the raw `BenchmarkLog` rows, the
  proxy log entries showing the provider's cache hit count,
  and the shell scripts to reproduce the test.

## How to add a new test

1. Create a subdirectory: `test/<test_name>_<YYYY_MM_DD>/`
2. Add the shell scripts in the order they should be run
   (`01_setup.sh`, `02_run.sh`, `03_analyze.sh`, etc.)
3. Export any relevant data (DB rows, logs, raw responses)
   as CSV / JSON / TXT files alongside the scripts
4. Write a `README.md` in the subdirectory explaining:
   - **What** was tested
   - **Why** this test exists
   - **How** to reproduce it
   - **What the data shows** — the actual numbers, with a
     one-line interpretation
   - **Limitations** — what this test does NOT cover
5. Never commit credentials, virtual keys, real server URLs, or
   passwords. Use `${VIRTUAL_KEY}` and placeholder URLs.

## Principles

- **Real data only.** We don't ship synthetic test fixtures. If
  a test was run against the production proxy, the data in
  this directory is the actual bytes the proxy emitted.
- **Scripts over instructions.** Every test ships with
  numbered shell scripts that can be re-run end-to-end on a
  fresh server.
- **Honest about limitations.** Each test README documents
  what it does NOT cover.
- **Credentials stay out.** All virtual keys are replaced by
  `${VIRTUAL_KEY}` placeholders before commit.

## See also

- [`../README.md`](../README.md) — top-level project readme
- [`../proxy/optiagent/compressor_test.go`](../proxy/optiagent/compressor_test.go) — unit tests for the L3 compressor
- [`../proxy/optiagent/prefix_split_test.go`](../proxy/optiagent/prefix_split_test.go) — unit tests for the cache-preserving split
