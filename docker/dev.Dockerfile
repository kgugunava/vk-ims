# Linux toolchain for building and testing the repository on any host.
# The assignment is checked on Linux with GCC/Clang, so builds and sanitizer
# runs are verified here rather than with the host compiler.
# libclang-rt-18-dev holds Clang's sanitizer runtimes; without it
# `clang++ -fsanitize=address` fails to link on Ubuntu. make is there for the
# default "Unix Makefiles" generator used by the commands in the task.
FROM ubuntu:24.04

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        clang \
        clang-format \
        cmake \
        g++ \
        libclang-rt-18-dev \
        make \
        ninja-build \
        python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
