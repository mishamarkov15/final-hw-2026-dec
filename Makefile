.PHONY: init up down test proto

init:
	sh scripts/init-env.sh

up: init
	docker compose up --build -d

down:
	docker compose down

test:
	go test -race ./... -coverprofile=cover.out
	node --test google-sheets/tests/*.test.cjs

proto:
	GOBIN=$$(pwd)/.tools go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.5
	GOBIN=$$(pwd)/.tools go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
	PATH=$$(pwd)/.tools:$$PATH protoc -I api/proto --go_out=. --go_opt=module=github.com/mishamarkov15/final-hw-2026-dec --go-grpc_out=. --go-grpc_opt=module=github.com/mishamarkov15/final-hw-2026-dec api/proto/cashcraft/v1/cashcraft.proto
