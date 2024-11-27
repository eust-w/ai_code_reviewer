.PHONY: build run test clean docker-build docker-run build-lambda deploy-lambda

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
BINARY_NAME=cr-bot
LAMBDA_BINARY=bin/lambda

# Main build target
build:
	$(GOBUILD) -o $(BINARY_NAME) ./cmd/server

# Run the application
run:
	$(GORUN) ./cmd/server

# Run tests
test:
	$(GOTEST) -v ./...

# Clean build files
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(LAMBDA_BINARY)

# Build Docker image
docker-build:
	docker build -t cr-bot .

# Run Docker container
docker-run:
	docker run -p 3000:3000 --env-file .env cr-bot

# Build Lambda function
build-lambda:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 $(GOBUILD) -tags lambda.norpc -o $(LAMBDA_BINARY) ./cmd/lambda

# Build Linux AMD64 version
build-linux-amd64:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/cr-bot-linux-amd64 ./cmd/server

# Deploy Lambda function
deploy-lambda:
	make build-lambda
	serverless deploy

# Run GitHub Action locally
action:
	$(GORUN) ./cmd/github-action

# Install dependencies
deps:
	$(GOCMD) mod download

# Format code
fmt:
	$(GOCMD) fmt ./...

# Run linter
lint:
	golangci-lint run

# Help command
help:
	@echo "Available commands:"
	@echo "  make build         - Build the application"
	@echo "  make run           - Run the application"
	@echo "  make test          - Run tests"
	@echo "  make clean         - Clean build files"
	@echo "  make docker-build  - Build Docker image"
	@echo "  make docker-run    - Run Docker container"
	@echo "  make build-lambda  - Build Lambda function"
	@echo "  make deploy-lambda - Deploy Lambda function"
	@echo "  make action        - Run GitHub Action locally"
	@echo "  make deps          - Install dependencies"
	@echo "  make fmt           - Format code"
	@echo "  make lint          - Run linter"
