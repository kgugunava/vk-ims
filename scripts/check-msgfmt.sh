#!/usr/bin/env bash
# Full msgfmt check, meant to run inside the dev container:
#   scripts/in-docker.sh ./scripts/check-msgfmt.sh
#
# Steps: clang-format, then GCC and Clang in Release with warnings as errors,
# then GCC and Clang with AddressSanitizer + UndefinedBehaviorSanitizer.
# Stops at the first failing step.
set -euo pipefail

readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly src_dir="$repo_root/msgfmt"
readonly build_root="$repo_root/build-linux"

step() {
    printf '\n==> %s\n' "$*"
}

check_format() {
    step "clang-format"
    local files
    files="$(find "$src_dir/include" "$src_dir/src" "$src_dir/tests" \
        -type f \( -name '*.hpp' -o -name '*.cpp' \) 2>/dev/null | sort)"
    if [[ -z "$files" ]]; then
        echo "no sources yet"
        return
    fi
    # shellcheck disable=SC2086  # file names contain no spaces
    clang-format --dry-run --Werror $files
}

# build_and_test <name> <c++ compiler> <build type> [extra cmake args...]
build_and_test() {
    local name="$1" compiler="$2" build_type="$3"
    shift 3
    local build_dir="$build_root/$name"

    step "$name: configure"
    cmake -S "$src_dir" -B "$build_dir" -G Ninja \
        -DCMAKE_CXX_COMPILER="$compiler" \
        -DCMAKE_BUILD_TYPE="$build_type" \
        -DMSGFMT_WERROR=ON \
        "$@"

    step "$name: build"
    cmake --build "$build_dir"

    step "$name: test"
    ctest --test-dir "$build_dir" --output-on-failure
}

if [[ ! -f "$src_dir/CMakeLists.txt" ]]; then
    echo "error: $src_dir/CMakeLists.txt not found" >&2
    exit 1
fi

check_format
build_and_test gcc-release g++ Release
build_and_test clang-release clang++ Release
build_and_test gcc-asan g++ Debug -DMSGFMT_SANITIZE=ON
build_and_test clang-asan clang++ Debug -DMSGFMT_SANITIZE=ON

step "all checks passed"
