FROM node:18

WORKDIR /app

# Install dependencies
COPY package*.json ./
RUN npm install

# Copy application files
COPY . .

# Generate Prisma client and Build Next.js
RUN npx prisma generate
# Remove static export mode — production runs as a Next.js server (SSR),
# output:'export' is only used by the local-client static build script.
RUN sed -i "/output: 'export'/d" next.config.mjs
RUN npm run build

# Expose port
EXPOSE 3000

# Sync API keys from Postgres to Redis before starting Next.js.
# Without this, the dashboard can read keys but the proxy cannot
# validate them after a Redis flush (proxy reads keys from Redis,
# not Postgres, see proxy/internal/services/auth.go).
# If sync fails, we still start the dashboard so the operator can
# log in and fix things -- but the warning is loud.
CMD ["sh", "-c", "node sync_keys.js || echo '[WARN] sync_keys.js failed (continuing anyway)' ; npm start"]
