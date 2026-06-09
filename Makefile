.PHONY: dev-api build-web test build run release install-linux

dev-api:
	go run ./cmd/frpc-web

build-web:
	cd web && npm run build

test:
	go test ./...
	cd web && npm run build

build: build-web
	mkdir -p bin
	go build -tags embed -trimpath -o bin/frpc-web ./cmd/frpc-web

run: build-web
	go run ./cmd/frpc-web

release:
	sh scripts/build-release.sh

install-linux: build
	sh scripts/install-linux.sh
