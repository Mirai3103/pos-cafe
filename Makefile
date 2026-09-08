.PHONY: run build test test-integration test-all coverage fmt vet sqlc swagger tidy clean docker-up docker-down docker-logs

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f postgres

run:
	go run cmd/api/main.go

build:
	go build -o bin/api cmd/api/main.go

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test -v -race ./...

test-integration:
	TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -v -race -tags=integration ./...

test-all: test test-integration

coverage:
	TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -cover -tags=integration ./...

sqlc:
	sqlc generate

swagger:
	swag init -g cmd/api/main.go -o docs

tidy:
	go mod tidy

clean:
	rm -rf bin/ *.db*
