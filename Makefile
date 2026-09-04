.PHONY: dev ui build test

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
