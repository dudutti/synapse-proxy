FROM node:18

WORKDIR /app

# Install dependencies
COPY package*.json ./
RUN npm install

# Copy application files
COPY . .

# Generate Prisma client and Build Next.js
RUN npx prisma generate
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
