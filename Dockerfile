# Build stage — compile the Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o karasu ./cmd/karasu

# Run stage — minimal image with just the binary
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS requests to MusicBrainz etc.
RUN apk --no-cache add ca-certificates tzdata

# Copy the binary from the builder
COPY --from=builder /app/karasu .

# Copy migrations so they're available at runtime
COPY --from=builder /app/internal/db/migrations ./internal/db/migrations

EXPOSE 8080

CMD ["./karasu"]