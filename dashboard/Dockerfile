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
# The original sync_keys.js was a one-shot that failed silently
# when Redis was not yet up. Replaced with sync_keys_with_retry.js
# which retries up to 15 times with exponential backoff and
# handles transient connection errors. This ensures that after
# a container recreate, the proxy can always validate the
# virtual keys it sees.
#
# sync_keys.js is kept as a fallback in case the retry version
# has a future regression. If both fail, the dashboard still
# starts so the operator can log in and fix things -- but the
# warning is loud.
CMD ["sh", "-c", "node sync_keys_with_retry.js || (echo '[WARN] sync_keys_with_retry.js failed, falling back to sync_keys.js' && node sync_keys.js) || echo '[WARN] sync_keys.js also failed (continuing anyway, proxy will not be able to validate virtual keys until sync runs)' ; npm start"]
