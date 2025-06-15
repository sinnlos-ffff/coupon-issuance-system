docker compose down -v
docker compose up -d
docker compose exec server go test -v -count=1 ./internal/server 