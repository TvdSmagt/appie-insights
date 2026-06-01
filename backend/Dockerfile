FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Stamp the binary with the release version. Pass --build-arg VERSION=$(git
# describe --tags) at build time; defaults to "development" for unstamped builds.
ARG VERSION=development
RUN CGO_ENABLED=0 go build -ldflags "-X main.Version=${VERSION}" -o backend .

FROM golang:1.23-alpine AS test
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM alpine:3.21
RUN adduser -u 1000 -D appuser && mkdir -p /app/config && chown appuser:appuser /app/config
WORKDIR /app
COPY --from=builder /build/backend .
USER appuser
ENTRYPOINT ["/app/backend"]
