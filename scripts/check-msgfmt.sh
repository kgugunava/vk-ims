#!/usr/bin/env bash
# Full msgfmt check, meant to run inside the dev container:
#   scripts/in-docker.sh ./scripts/check-msgfmt.sh
#
# Steps: clang-format, then GCC and Clang in Release with warnings as errors,
# then GCC and Clang with AddressSanitizer + UndefinedBehaviorSanitizer, then
# GCC's stack-use-after-return check on the tests not labelled heavy (it is off
# for regular test runs, see msgfmt/tests/CMakeLists.txt), then the build
# commands exactly as the task gives them.
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
readonly no_uar="-DMSGFMT_TEST_ASAN_OPTIONS=detect_stack_use_after_return=0"
build_and_test gcc-asan g++ Debug -DMSGFMT_SANITIZE=ON "$no_uar"
build_and_test clang-asan clang++ Debug -DMSGFMT_SANITIZE=ON "$no_uar"

# Reconfiguring only changes the test environment, so nothing is rebuilt.
step "gcc-asan: stack use after return, tests not labelled heavy"
cmake -S "$src_dir" -B "$build_root/gcc-asan" \
    -DMSGFMT_TEST_ASAN_OPTIONS=detect_stack_use_after_return=1 >/dev/null
ctest --test-dir "$build_root/gcc-asan" --output-on-failure -LE heavy

# The exact commands from the task: default generator and compiler, none of
# our options. A reviewer may also pass the sanitizers through
# CMAKE_CXX_FLAGS instead of MSGFMT_SANITIZE; both must build and pass.
# Warnings are checked by the steps above (-Werror); here the build output is
# also grepped, because without MSGFMT_WERROR a warning would not fail it.
task_build() {
    local name="$1"
    shift
    local build_dir="$build_root/$name"
    rm -rf "$build_dir"

    step "$name: commands from the task"
    cmake -S "$src_dir" -B "$build_dir" -DCMAKE_BUILD_TYPE=Release "$@" >/dev/null
    local log
    log="$(cmake --build "$build_dir" 2>&1)"
    if grep -qi "warning" <<<"$log"; then
        echo "$log"
        echo "error: the build printed warnings" >&2
        exit 1
    fi
    ctest --test-dir "$build_dir" --output-on-failure
}

task_build task-plain
task_build task-sanitizer-flags "-DCMAKE_CXX_FLAGS=-fsanitize=address,undefined"

step "all checks passed"
