FROM golang:1.21-alpine

WORKDIR /app

# Copy all Go source files
COPY . .

# Download dependencies
RUN go mod tidy

# Build the proxy binary
RUN go build -o proxy-server ./cmd/server/main.go

# Expose proxy port
EXPOSE 8080

# Run the proxy
CMD ["./proxy-server"]
