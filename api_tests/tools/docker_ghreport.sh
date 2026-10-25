#!/usr/bin/env bash
# Runs the ghreport Docker image as if it were the binary, so that the suite
# tests the image itself:
#
#   docker build -t ghreport service/
#   GHREPORT_BIN=$PWD/api_tests/tools/docker_ghreport.sh \
#     GITHUB_API_BASE_URL=http://127.0.0.1:8080 pytest api_tests -q
#
# The directories of --cache-dir, --trace and --out are mounted at the same
# paths inside the container. A GITHUB_API_BASE_URL on this machine is made
# reachable: host networking on Linux, host.docker.internal elsewhere.
# GHREPORT_IMAGE selects another image tag.
set -euo pipefail

readonly image="${GHREPORT_IMAGE:-ghreport}"

mounts=()
mounted=" "
mount_dir_of() {
    local path="$1"
    [[ -z "$path" || "$path" == "-" ]] && return
    # ghreport may create the directory itself (--cache-dir nested/cache), so
    # mount the nearest ancestor that already exists.
    local dir
    dir="$(dirname "$path")"
    while [[ ! -d "$dir" ]]; do
        dir="$(dirname "$dir")"
    done
    dir="$(cd "$dir" && pwd)"
    # Docker rejects the same mount point twice.
    if [[ "$mounted" != *" $dir "* ]]; then
        mounts+=(-v "$dir:$dir")
        mounted+="$dir "
    fi
}

args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
    case "${args[i]}" in
        --cache-dir | --trace | --out | -cache-dir | -trace | -out)
            mount_dir_of "${args[i + 1]:-}"
            ;;
        --cache-dir=* | --trace=* | --out=*)
            mount_dir_of "${args[i]#*=}"
            ;;
    esac
done

base="${GITHUB_API_BASE_URL:-}"
network=()
if [[ "$(uname -s)" == Linux ]]; then
    network=(--network host)
else
    base="${base/127.0.0.1/host.docker.internal}"
    base="${base/localhost/host.docker.internal}"
fi

env_args=()
[[ -n "$base" ]] && env_args+=(-e "GITHUB_API_BASE_URL=$base")
# Passed by name, so the token never appears on the command line.
[[ -n "${GITHUB_TOKEN:-}" ]] && env_args+=(-e GITHUB_TOKEN)

# The ${arr[@]+...} form keeps empty arrays working under `set -u` in the
# bash 3.2 that ships with macOS. --user keeps files in mounted directories
# owned by the caller.
exec docker run --rm -i --user "$(id -u):$(id -g)" \
    ${network[@]+"${network[@]}"} ${mounts[@]+"${mounts[@]}"} ${env_args[@]+"${env_args[@]}"} \
    "$image" "$@"
