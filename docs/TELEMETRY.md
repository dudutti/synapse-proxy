# Synapse Proxy Telemetry Architecture

Synapse Proxy is built not just for performance, but for **total observability**. We provide granular telemetry at multiple levels: per-request logs, real-time streams, and aggregated global statistics.

Whether you deploy Synapse Proxy as a self-hosted Enterprise Gateway or run the SaaS platform, the telemetry system runs silently in the background without blocking the critical path.

## ðŸ—ï¸ How Telemetry Works

Synapse Proxy's telemetry is completely decoupled from the proxy router (`HandleProxyRequest`).

### 1. The Real-Time Stream (Redis)
When a request finishes, the Proxy immediately pushes the raw metrics (tokens sent, tokens optimized, duration, cache hits, provider, etc.) into a **Redis Stream** (`synapse:telemetry:logs`) using a lightweight, non-blocking asynchronous call.

### 2. The Ingestion Worker (Go)
A background Go routine `ConsumeTelemetryWorker()` continuously reads from the Redis Stream in batches. It calculates the financial savings (using our dynamic pricing module) and safely persists the logs into the persistent **PostgreSQL** database. This architecture ensures that sudden spikes in LLM traffic won't crash the database.

### 3. The Global Stats Worker (Go)
A secondary background worker `GlobalStatsWorker()` runs a lightweight cron job every 5 minutes. It queries PostgreSQL to build an aggregated snapshot of the entire proxy's health:
- **Total Requests Processed**
- **Net Wealth Preserved (Dollars saved globally)**
- **Global Compression Ratios**
- **Cache Hit Distribution (L1 / L2 / L3)**
- **Hourly Traffic Activity (Last 24h)**
- **Top Models Usage**

This JSON snapshot is stored back in Redis (`synapse:global_stats`) with a TTL.

---

## ðŸŒŽ Using Telemetry in Self-Hosted Deployments

If you are a developer self-hosting `Synapse Proxy-proxy` locally or inside your VPC, the telemetry data is highly valuable:

1. **Custom Dashboards**: You can connect your own BI tools (like Metabase, Superset) or internal dashboards directly to the PostgreSQL instance to visualize your team's exact LLM usage and savings. (Note: The official Synapse Proxy Dashboard is part of our SaaS offering).
2. **Prometheus / Grafana**: The JSON snapshot in Redis (`synapse:global_stats`) can be easily scraped by external monitoring tools to build custom alerts and Grafana dashboards.
3. **Internal Transparency**: If you host internal hackathons or deploy agents across multiple departments, exposing the `global-stats` endpoint internally allows everyone to see the collective savings in real-time.

By default, the global stats can be fetched via:
`GET http://<proxy-host>:3000/api/public/global-stats` 
(If the Next.js API is exposed).

This ensures complete transparency over what the gateway is doing behind the scenes!
