#!/usr/bin/env sh
set -eu

# Update only the new-api application container.
# Redis/PostgreSQL are left running by default.

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
SERVICE_NAME="${SERVICE_NAME:-new-api}"
BRANCH="${BRANCH:-wukejing}"
RESTART_DEPS="${RESTART_DEPS:-0}"
BUILD_IMAGE="${BUILD_IMAGE:-1}"

cd "$ROOT_DIR"

log() {
  printf '[update-new-api] %s\n' "$*"
}

die() {
  printf '[update-new-api] ERROR: %s\n' "$*" >&2
  exit 1
}

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  die "Docker Compose is not installed or not available in PATH"
fi

if [ ! -f "$COMPOSE_FILE" ]; then
  die "Compose file not found: $COMPOSE_FILE"
fi

if [ ! -f ".env" ]; then
  die ".env not found. Create it from .env.example and set production passwords first"
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  die "Working tree has uncommitted tracked changes. Commit or stash them before updating"
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" != "$BRANCH" ]; then
  die "Current branch is $CURRENT_BRANCH, expected $BRANCH. Set BRANCH=$CURRENT_BRANCH to override"
fi

log "Fetching latest code from origin/$BRANCH"
git fetch origin "$BRANCH"

LOCAL_COMMIT="$(git rev-parse HEAD)"
REMOTE_COMMIT="$(git rev-parse "origin/$BRANCH")"
BASE_COMMIT="$(git merge-base HEAD "origin/$BRANCH")"

if [ "$LOCAL_COMMIT" = "$REMOTE_COMMIT" ]; then
  log "Code is already up to date"
elif [ "$LOCAL_COMMIT" = "$BASE_COMMIT" ]; then
  log "Fast-forwarding to origin/$BRANCH"
  git merge --ff-only "origin/$BRANCH"
else
  die "Local branch has diverged from origin/$BRANCH. Resolve manually before deploying"
fi

COMPOSE_ARGS="-f $COMPOSE_FILE"
UP_ARGS="-d"

if [ "$BUILD_IMAGE" = "1" ]; then
  UP_ARGS="$UP_ARGS --build"
fi

if [ "$RESTART_DEPS" != "1" ]; then
  UP_ARGS="$UP_ARGS --no-deps"
fi

log "Updating service: $SERVICE_NAME"
# shellcheck disable=SC2086
$COMPOSE $COMPOSE_ARGS up $UP_ARGS "$SERVICE_NAME"

log "Current containers"
# shellcheck disable=SC2086
$COMPOSE $COMPOSE_ARGS ps

log "Done. View logs with: $COMPOSE $COMPOSE_ARGS logs -f $SERVICE_NAME"
