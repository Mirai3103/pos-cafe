.PHONY: run build test sqlc swagger tidy clean docker-up docker-down docker-logs

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

test:
	go test -v -race ./...

sqlc:
	sqlc generate

swagger:
	swag init -g cmd/api/main.go -o docs

tidy:
	go mod tidy

clean:
	rm -rf bin/ *.db*
