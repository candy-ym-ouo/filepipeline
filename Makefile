.PHONY: build test vet verify run-api run-worker stats clean

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

stats:
	@echo "non-test Go files: $$(find cmd internal -name '*.go' ! -name '*_test.go' | wc -l | tr -d ' ')"
	@echo "non-test Go lines: $$(find cmd internal -name '*.go' ! -name '*_test.go' -print0 | xargs -0 wc -l | tail -1 | awk '{print $$1}')"

verify: build vet test stats

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

clean:
	rm -rf data
