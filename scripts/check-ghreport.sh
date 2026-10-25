#!/usr/bin/env bash
# Full ghreport check in Linux with the oldest Go the task allows (1.22):
# gofmt, go vet, go test -race. Run from the repository root on the host:
#   scripts/check-ghreport.sh
# Go 1.22 also catches standard library API from newer releases, which a
# newer local toolchain would accept despite "go 1.22" in go.mod.
set -euo pipefail

readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly image="golang:1.22"

# Module and build caches live in named volumes, so repeated runs are fast.
exec docker run --rm \
    -v "$repo_root/service":/src -w /src \
    -v vk-ims-go-mod:/go/pkg/mod -v vk-ims-go-build:/root/.cache/go-build \
    "$image" sh -euc '
        go version
        echo "==> gofmt"
        unformatted="$(gofmt -l .)"
        if [ -n "$unformatted" ]; then echo "$unformatted"; exit 1; fi
        echo "==> go vet"
        go vet ./...
        echo "==> go test -race"
        go test -race -count=1 ./...
        echo "==> all checks passed"
    '
