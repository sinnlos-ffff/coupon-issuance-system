#!/bin/bash
set -e

until pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do
    echo "Waiting for PostgreSQL to be ready..."
    sleep 2
done

for migration in /docker-entrypoint-initdb.d/migrations/*.sql; do
    echo "Running migration: $migration"
    psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$migration"
done

echo "Database initialization completed" 