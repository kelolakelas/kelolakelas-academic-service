.PHONY: run test swagger migrate-up migrate-down seed create-migration create-seeder

# Pinned to the swag version that generated docs/; keep it equal to the
# github.com/swaggo/swag version in go.mod.
SWAG_VERSION ?= v1.16.4

run:
	go run ./cmd/server

test:
	go test ./...

# Regenerates docs/{docs.go,swagger.json,swagger.yaml} from the handler annotations.
# cmd/server/swagger_contract_test.go fails when docs/ drifts from the registered routes.
swagger:
	go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init -g cmd/server/main.go -o docs --parseDependency
	gofmt -w docs/docs.go

migrate-up:
	go run ./cmd/migrate -direction up

migrate-down:
	go run ./cmd/migrate -direction down -steps 1

seed:
	@test -d seeders || (echo "no seeders/ directory: create one first with 'make create-seeder name=<seeder>'" >&2; exit 1)
	go run ./cmd/seed -dir seeders

create-migration:
	test -n "$(name)" || (echo "name is required, for example: make create-migration name=add_indexes" >&2; exit 1)
	go run ./cmd/create-migration -name "$(name)"

create-seeder:
	test -n "$(name)" || (echo "name is required, for example: make create-seeder name=demo_accounts" >&2; exit 1)
	go run ./cmd/create-seeder -name "$(name)"
