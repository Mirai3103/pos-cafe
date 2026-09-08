.PHONY: run build test sqlc swagger tidy clean

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
