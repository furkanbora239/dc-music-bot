.PHONY: all build run test docker-build docker-up docker-down clean

BINARY_NAME=dc-music-bot
BUILD_DIR=bin

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/bot

run:
	go run ./cmd/bot

test:
	go test -v -race ./...

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

clean:
	rm -rf $(BUILD_DIR) *.test *.out
