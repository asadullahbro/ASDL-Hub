.PHONY: build run clean test build-shortcuts

BINARY_NAME=asdl-hub
BUILD_DIR=bin

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) cmd/hub/main.go

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

run-dev:
	go run cmd/hub/main.go

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -f asdl.db

deps:`
	go mod download
	go mod tidy

build-shortcuts:
	cd scripts/shortcuts && npm install && npm run build