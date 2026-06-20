# Stage 1: Build Rust Static Library
FROM rust:alpine AS rust-builder
RUN apk add --no-cache musl-dev gcc make
WORKDIR /rust-build
COPY rust-embedder ./rust-embedder
WORKDIR /rust-build/rust-embedder
RUN cargo build --release

# Stage 2: Download Model Weights
FROM alpine AS model-downloader
RUN apk add --no-cache curl
WORKDIR /models
RUN curl -f -L -o config.json https://huggingface.co/sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/config.json && \
    curl -f -L -o tokenizer.json https://huggingface.co/sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/tokenizer.json && \
    curl -f -L -o model.safetensors https://huggingface.co/sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/model.safetensors

# Stage 3: Build Go Binary with CGO
FROM golang:1.21-alpine AS go-builder
RUN apk add --no-cache gcc musl-dev libc-dev
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# Copy compiled Rust static library to the directory expected by CGO LDFLAGS
COPY --from=rust-builder /rust-build/rust-embedder/target/release/librust_embedder.a /app/rust-embedder/target/release/librust_embedder.a
ENV GOCACHE=/tmp/gocache-empty
ENV GOFLAGS=-trimpath
ENV CGO_ENABLED=1
RUN mkdir -p /tmp/gocache-empty && go build -o proxy-server ./cmd/server/main.go && chmod +x proxy-server

# Stage 4: Runtime Image
FROM alpine
RUN apk add --no-cache ca-certificates libgcc
WORKDIR /app
COPY --from=go-builder /app/proxy-server ./
COPY --from=model-downloader /models /app/models/paraphrase-multilingual-MiniLM-L12-v2
EXPOSE 8080
CMD ["./proxy-server"]

