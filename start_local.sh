#!/usr/bin/env bash
# Start the app locally: export everything in .env, then run the server from
# source via `make run`. Migrations apply automatically on boot.
set -euo pipefail

cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
	echo "No .env found in $(pwd); copy .env.example to .env and fill it in." >&2
	exit 1
fi

set -a
. ./.env
set +a

exec make run
