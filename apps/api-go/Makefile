.PHONY: build test vet run image

build:
	CGO_ENABLED=0 go build -o bin/pace-api ./cmd/api

test:
	go test -count=1 ./...

vet:
	go vet ./...

run:
	go run ./cmd/api

image:
	docker build -t pace-api-go:local .
