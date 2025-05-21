.PHONY: benchmark
benchmark:
	go test -cpu 1,2,4,8,16 -bench . ./...

.PHONY: cover
cover:
	go tool cover -html cover.out

.PHONY: lint
lint:
	golangci-lint run

.PHONY: test
test:
	go test -coverprofile cover.out -shuffle on ./...

.PHONY: test-down
test-down:
		docker compose down

.PHONY: test-up
test-up:
	docker compose up -d
