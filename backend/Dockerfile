# Stage 1: builder
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency manifests first to leverage layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

# Stage 2: runtime
FROM alpine:3.19

# Install CA certificates for outbound HTTPS calls (e.g. AWS APIs).
RUN apk --no-cache add ca-certificates

COPY --from=builder /server /server

EXPOSE 8080

CMD ["/server"]
