# --- Build stage ---
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static, stripped binary. Templates, static assets and migrations are embedded.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /durooma ./cmd/server

# --- Runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
USER app
COPY --from=build /durooma /usr/local/bin/durooma
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/durooma"]
