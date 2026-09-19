.PHONY: run build test tidy docker-up docker-down fmt vet

# Local dev: load .env for the database connection and provider settings.
run:
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a; \
	go run ./cmd/server

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o durooma ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

# Bring up Postgres + app via docker-compose.
docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

# Start only a Postgres for local `make run`.
db:
	docker run --rm -e POSTGRES_USER=durooma -e POSTGRES_PASSWORD=durooma \
		-e POSTGRES_DB=durooma -p 5432:5432 postgres:16-alpine
