VERSION ?= $(shell git rev-parse --short HEAD)

# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.out
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race $(COVERAGE_ARGS)

# 3rd party tools
GOLINT      := go run golang.org/x/lint/golint@latest
REFLEX      := go run github.com/cespare/reflex@v0.3.1
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@2023.1.3

# Extract golangci-lint version from GitHub actions workflow
GOLANGCI_LINT_VERSION ?= $(shell grep -A2 'uses: golangci/golangci-lint-action' .github/workflows/lint.yaml  | grep version: | awk '{print $$NF}')

test:
	go test $(TEST_ARGS) ./...
.PHONY: test

# Test command to run for continuous integration, which includes code coverage
# based on codecov.io's documentation:
# https://github.com/codecov/example-go/blob/b85638743b972bd0bd2af63421fe513c6f968930/README.md
testci:
	go test $(TEST_ARGS) $(COVERAGE_ARGS) ./...
.PHONY: testci

testcover: testci
	go tool cover -html=$(COVERAGE_PATH)
.PHONY: testcover

lint:
	test -z "$$(gofmt -d -s -e .)" || (echo "Error: gofmt failed"; gofmt -d -s -e . ; exit 1)
	go vet ./...
	$(GOLINT) -set_exit_status ./...
	$(STATICCHECK) ./...
.PHONY: lint

lintci: lint
	docker run \
		--rm \
		--volume $(pwd):/app \
		--volume $(HOME)/.cache/golangci-lint/$(GOLANGCI_LINT_VERSION):/root/.cache \
		--workdir /app \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run -v

clean:
	rm -rf $(COVERAGE_PATH)
.PHONY: clean
