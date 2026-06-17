FROM golang:1.21-alpine

WORKDIR /app

# CACHEBUST forces a clean Go module rebuild every time this arg
# changes (e.g. via --build-arg CACHEBUST=$(date +%s)). Without this,
# Docker's `--no-cache` only invalidates the layer cache, not Go's
# internal GOCACHE inside the layer — and we end up shipping a stale
# binary that does not include the latest .go source.
ARG CACHEBUST=default

# Copy go.mod/go.sum first so the dep download is its own layer
COPY go.mod go.sum* ./
RUN go mod download

# Copy all source — any change to a .go file invalidates this layer
COPY . .

# Disable Go's internal build cache by pointing it at an empty directory.
# (Go refuses to use /dev/null because it's a character device, not a dir.)
ENV GOCACHE=/tmp/gocache-empty
ENV GOFLAGS=-trimpath

# Rebuild from absolute zero. GOCACHE points at an empty dir so Go
# compiles every package from source every time — no stale intermediate
# objects survive between builds. Combined with --no-cache and the
# CACHEBUST ARG, we get a guaranteed fresh binary on every deploy.
RUN mkdir -p /tmp/gocache-empty && go build -o proxy-server ./cmd/server/main.go && chmod +x proxy-server

EXPOSE 8080

CMD ["./proxy-server"]
