#!/bin/sh
# Optional desktop browser prototype; the native Kindle app does not need this.
set -eu
cd "$(dirname "$0")"
INK_PYTHON_BIN="${INK_PYTHON_BIN:-python3}"
INK_HOST="${INK_HOST:-127.0.0.1}"
INK_PORT="${INK_PORT:-8765}"
echo "Preview: http://${INK_HOST}:${INK_PORT}/ (Control-C to stop)"
exec "$INK_PYTHON_BIN" -m ink serve --host "$INK_HOST" --port "$INK_PORT"
