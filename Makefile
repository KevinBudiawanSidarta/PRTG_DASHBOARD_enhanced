.PHONY: db api collector worker scheduler jetsync web test

db:
	docker compose up -d postgres
api:
	go run ./services/api/cmd
collector:
	go run ./services/collector/cmd
worker:
	go run ./services/worker/cmd
scheduler:
	go run ./services/scheduler/cmd
jetsync:
	go run ./services/jetsync/cmd
web:
	cd apps/web && npm install && npm run dev
test:
	go test ./... -race -cover
