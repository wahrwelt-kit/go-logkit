FUZZTIME ?= 10s

.PHONY: test test-race test-fuzz test-bench fmt vet lint mockery cover tidy

test:
	go test ./...

test-race:
	go test -race ./...

test-fuzz:
	go test . -run '^$$' -fuzz='^FuzzSanitizeMsg$$' -fuzztime=$(FUZZTIME)
	go test . -run '^$$' -fuzz='^FuzzSanitizeFields$$' -fuzztime=$(FUZZTIME)
	go test . -run '^$$' -fuzz='^FuzzSensitiveKeyMatching$$' -fuzztime=$(FUZZTIME)
	go test . -run '^$$' -fuzz='^FuzzSlogAttrs$$' -fuzztime=$(FUZZTIME)

test-bench:
	go test -bench=. ./...

fmt:
	gofmt -w .
	goimports -w .

vet:
	go vet ./...

lint:
	golangci-lint run --fix ./...

mockery:
	mockery --config .mockery.yml

cover:
	go test -coverprofile=coverage.out . && go tool cover -html=coverage.out

tidy:
	go mod tidy
