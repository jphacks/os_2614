# .env があれば読み込んで環境変数にする。
-include .env
export

.PHONY: run env redis-up redis-down db-reset db-schema test test-integration lint tunnel codemap

run:
	go run .

# .env.example から .env を作り、鍵を乱数で埋める（既にあれば何もしない）。
env:
	@test -f .env && echo ".env already exists" || ( \
	  sed -e "s|^CHAIN_SECRET_KEY=.*|CHAIN_SECRET_KEY=$$(openssl rand -base64 32)|" \
	      -e "s|^CHAIN_JWT_KEY=.*|CHAIN_JWT_KEY=$$(openssl rand -base64 32)|" \
	      .env.example > .env && echo "created .env" )

redis-up:
	docker compose up -d redis

redis-down:
	docker compose down

db-reset:
	go run ./cmd/dbreset

db-schema:
	go run ./cmd/dbreset -no-seed

test:
	go test ./...

# 結合テストは Redis の DB 15 を共有するので直列に実行する（15-testing.md §3）。
test-integration:
	CHAIN_TEST_REDIS_URL=$${CHAIN_TEST_REDIS_URL:-redis://localhost:6379/15} go test -p 1 -tags=integration ./...

# time.Now は internal/clock 以外で呼ばない（15-testing.md §2）。
lint:
	go vet ./...
	@! grep -rn --include='*.go' 'time\.Now(' . | grep -v '^./internal/clock/' || (echo "time.Now() is only allowed in internal/clock" && exit 1)

tunnel:
	./scripts/tunnel.sh

# 機能・コントローラー・パスの一覧（specification/backend/18-code-map.md）を作り直す。
codemap:
	python3 scripts/codemap.py > ../specification/backend/18-code-map.md
