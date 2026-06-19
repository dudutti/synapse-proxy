# Synapse Proxy Deployment Guide

This guide outlines how to deploy Synapse Proxy for a production environment capable of handling thousands of requests per second.

## 1. Environment Setup

You need to define the following environment variables in a `.env` file:
```env
# Database & Redis
DATABASE_URL="postgresql://user:password@host:5432/Synapse Proxy_db?schema=public&sslmode=require"
REDIS_URL="redis://user:password@host:6379"

# Security
NEXTAUTH_SECRET="your-random-secret"
NEXTAUTH_URL="https://your-dashboard-domain.com"
ENCRYPTION_KEY="32-character-hex-string-for-api-keys"

# ONNX Embedder
ONNX_API_URL="http://onnx-embedder:8000/embed"
```

## 2. Docker Swarm / Kubernetes (Recommended)

To handle massive scale, the architecture should be deployed as microservices using Kubernetes or Docker Swarm.

### Components to Scale Independently:
1. **Go Proxy (Data Plane):** Highly scalable. Can be load-balanced easily. Needs very little RAM.
2. **ONNX Embedder:** Compute-heavy. Scale this horizontally based on semantic cache load. Consider using HuggingFace TEI (Text Embeddings Inference) written in Rust for production GPU acceleration.
3. **Next.js Dashboard:** Standard web scaling. Can be deployed on Vercel or AWS Amplify.
4. **Postgres & Redis:** Use managed services (e.g., AWS RDS for Postgres, AWS ElastiCache / Redis Enterprise for VSS).

## 3. Simple Docker Compose (Single Node)

For testing or small-scale production, use the provided `docker-compose.yml`.

```bash
# Start all services
docker-compose up -d --build

# Push Prisma Schema to Postgres
docker-compose exec -T nextjs npx prisma db push
```

## 4. Migrating ONNX to Rust (TEI)

The current ONNX embedder is written in Python (FastAPI). For production at scale, it is highly recommended to replace it with HuggingFace TEI:
```yaml
onnx-embedder:
  image: ghcr.io/huggingface/text-embeddings-inference:cpu-1.2
  command: --model-id sentence-transformers/all-MiniLM-L6-v2 --port 8000
  ports:
    - "8000:8000"
```
This requires zero changes to the Go proxy, as long as the `/embed` endpoint contract is maintained.
