APP_NAME := forgemill
GO_CMD := go
NPM_CMD := npm

.PHONY: all build run dev dev-frontend dev-backend clean docker docker-up docker-down deps frontend backend test lint

all: build

## Dependencies
deps:
	cd frontend && $(NPM_CMD) install
	$(GO_CMD) mod download

## Build
build: frontend backend

frontend:
	cd frontend && $(NPM_CMD) run build

backend:
	# MED-38: Quote variable expansions to prevent shell injection
	CGO_ENABLED=0 $(GO_CMD) build -ldflags="-s -w" -o "bin/$(APP_NAME)" ./cmd/forgemill

## Development
# 9.4: Use trap to kill all background processes on Ctrl+C
dev:
	@trap 'kill 0' EXIT; \
	$(GO_CMD) run ./cmd/forgemill & \
	cd frontend && $(NPM_CMD) run dev & \
	wait

dev-frontend:
	cd frontend && $(NPM_CMD) run dev

dev-backend:
	$(GO_CMD) run ./cmd/forgemill

## Run
run: build
	"./bin/$(APP_NAME)"

## Docker
docker:
	docker build -t "$(APP_NAME)" .

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

## Test & Lint (9.8)
test:
	$(GO_CMD) test ./...

lint:
	$(GO_CMD) vet ./...
	cd frontend && npx tsc --noEmit

## Clean
clean:
	rm -rf bin/
	rm -rf frontend/dist
	rm -rf frontend/node_modules
