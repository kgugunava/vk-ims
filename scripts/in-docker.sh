#!/usr/bin/env bash
# Runs a command inside the Linux dev container with the repository mounted
# at /src. Builds the image on first use.
#
#   scripts/in-docker.sh ./scripts/check-msgfmt.sh
#   scripts/in-docker.sh bash
set -euo pipefail

readonly image="vk-ims-dev"
readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! docker image inspect "$image" >/dev/null 2>&1; then
    docker build -q -t "$image" -f "$repo_root/docker/dev.Dockerfile" "$repo_root/docker" >/dev/null
fi

tty_flags=()
if [[ -t 0 && -t 1 ]]; then
    tty_flags=(-it)
fi

# Run as the host user so build directories on the mount are not owned by root
# on Linux hosts (Docker Desktop on macOS maps ownership itself).
# The ${arr[@]+...} form keeps an empty array working under `set -u` in the
# bash 3.2 that ships with macOS.
exec docker run --rm ${tty_flags[@]+"${tty_flags[@]}"} \
    --user "$(id -u):$(id -g)" \
    -v "$repo_root":/src -w /src \
    "$image" "$@"
