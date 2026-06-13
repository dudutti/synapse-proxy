# Contributing to OptiToken

Thank you for your interest in contributing to OptiToken! We welcome contributions from the community to help make our blazing-fast semantic cache and API optimizer even better.

This document outlines the process for setting up the development environment, making changes, and submitting a Pull Request.

## 🏗️ Project Structure

OptiToken is designed with an **Open Core** model. The open-source repository contains the core proxy engine and embedding service:

- `/proxy`: The high-performance Go reverse proxy.
- `/proxy/onnx`: The Python FastAPI service that generates embeddings using ONNX (`paraphrase-multilingual-MiniLM-L12-v2`).
- `/dashboard`: The Next.js Enterprise Dashboard (may contain proprietary features).
- `/docs`: General architecture and deployment documentation.

---

## 🛠️ Local Development Setup

To contribute to the codebase, you will need to run the proxy and its dependencies locally.

### Prerequisites

- **Go 1.21+** (for compiling the proxy)
- **Node.js 20+** (for the dashboard)
- **Docker & Docker Compose** (for Redis Stack and the ONNX service)

### 1. Start External Services (Redis & Embedder)

The proxy relies on **Redis Stack** (for Vector Search) and the **Python ONNX Embedder**. The easiest way to start them is via Docker:

```bash
# From the root directory
docker compose up -d optitoken-redis optitoken-onnx
```

### 2. Run the Go Proxy Locally

Instead of running the proxy in Docker, you can run it directly with Go to enable fast iteration and debugging:

```bash
cd proxy
go mod tidy
go run main.go
```

The proxy will start on `http://localhost:8080`.

### 3. Run the Next.js Dashboard

If you are contributing to the UI or API routes:

```bash
cd dashboard

# Install dependencies
npm install

# Setup Prisma Database (Requires a PostgreSQL instance)
# Ensure DATABASE_URL is set in dashboard/.env
npx prisma generate
npx prisma db push

# Start the dev server
npm run dev
```

The dashboard will be available at `http://localhost:3000`.

---

## 💡 Contribution Guidelines

### Submitting Issues
If you find a bug or have a feature request, please open an Issue. Provide as much context as possible:
- Steps to reproduce the bug.
- Expected vs. Actual behavior.
- Logs from the Go proxy or Docker containers.

### Submitting Pull Requests
1. **Fork** the repository and create your branch from `main`.
2. **Branch Naming**: Use prefixes like `feat/`, `fix/`, `docs/`, or `refactor/` (e.g., `feat/anthropic-caching`).
3. **Write Clear Code**: Follow standard Go (`gofmt`) and TypeScript conventions.
4. **Testing**: Test your changes locally. If you add new caching logic, ensure you haven't broken the streaming (`SSE`) functionality.
5. **Commit Messages**: Write descriptive commit messages.
6. **Open a PR**: Point your PR to the `main` branch. Provide a clear description of what the PR solves and link any related issues.

### Adding Support for New LLM Providers
OptiToken currently supports OpenAI, Anthropic, Gemini, DeepSeek, and Minimax. 
To add a new provider:
1. Update the `executeRequest` switch statement in `proxy/main.go`.
2. Ensure the JSON request/response schema maps correctly for token extraction (`extractUsage`).
3. Add the provider to the Dashboard dropdowns (`dashboard/app/settings/page.tsx`).

## ⚖️ Code of Conduct

By participating in this project, you agree to maintain a respectful and welcoming environment for everyone. Harassment or abusive behavior will not be tolerated.

---
*Happy Optimizing! 🚀*
