.PHONY: dev ui build test docker-build docker-up docker-down docker-logs

ui:
	cd internal/server/ui && npm install && npm run build

dev:
	go run ./cmd/lark-ob start

build: ui
	mkdir -p bin
	go build -o bin/lark-ob ./cmd/lark-ob

test:
	go test ./...
	cd internal/server/ui && npm run build

docker-build:
	docker-compose build

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f lark-ob
