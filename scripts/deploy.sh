#!/usr/bin/env bash
# Deploy one image tag on the host. Run from the directory that holds
# compose.prod.yaml, .env.production, and this script.
#
#   IMAGE_TAG=v1.0.0 bash scripts/deploy.sh
#
# Backs up the database, migrates, swaps the container, and rolls back to the
# last known good tag when the health gate fails.
set -euo pipefail

: "${IMAGE_TAG:?IMAGE_TAG is required}"

COMPOSE_FILE="${COMPOSE_FILE:-compose.prod.yaml}"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-type-fast}"
BACKUP_DIR="${BACKUP_DIR:-backups}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:3001/readyz}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
STATE_FILE="${STATE_FILE:-.last-good-tag}"

compose() { docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }

echo "deploying ${IMAGE_TAG}"

if [ -z "${SKIP_BACKUP:-}" ]; then
  mkdir -p "$BACKUP_DIR"
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  dump="${BACKUP_DIR}/kit-${stamp}.sql"
  echo "backing up the database to ${dump}"
  # shellcheck disable=SC2016
  compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > "$dump"
  echo "backup written: $(wc -c < "$dump") bytes"
fi

export IMAGE_TAG

echo "pulling the image"
compose pull --quiet user migrate redis

echo "applying migrations"
compose run --rm migrate up

echo "starting the API"
compose up -d --no-deps user redis

echo "waiting for ${HEALTH_URL}"
deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
until curl -fsS "$HEALTH_URL" > /dev/null; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "health gate failed after ${HEALTH_TIMEOUT}s" >&2
    previous=""
    [ -f "$STATE_FILE" ] && previous="$(cat "$STATE_FILE")"
    if [ -n "$previous" ] && [ "$previous" != "$IMAGE_TAG" ]; then
      echo "rolling back to ${previous}" >&2
      IMAGE_TAG="$previous" compose up -d --no-deps user redis
      if curl -fsS "$HEALTH_URL" > /dev/null; then
        echo "rollback to ${previous} is healthy" >&2
      else
        echo "rollback to ${previous} is also unhealthy; investigate" >&2
      fi
    else
      echo "no previous tag recorded; leaving the container up for inspection" >&2
    fi
    exit 1
  fi
  sleep 2
done

echo "$IMAGE_TAG" > "$STATE_FILE"
echo "deployed ${IMAGE_TAG}"
