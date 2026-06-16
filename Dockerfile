# Build stage
FROM golang:1.26 AS builder

WORKDIR /src

# Download dependencies first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build the binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/telegram-gateway .

# Final execution stage
FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /app/telegram-gateway .

# Expose the gateway port
EXPOSE 8000

# Run the binary
ENTRYPOINT ["/app/telegram-gateway"]
