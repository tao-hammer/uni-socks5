#!/bin/sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
if ! command -v go >/dev/null 2>&1; then
	echo "go is not installed or not in PATH" >&2
	exit 1
fi
go mod tidy
exec go run ./cmd/mkipk
