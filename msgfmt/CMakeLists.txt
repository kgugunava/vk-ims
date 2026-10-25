cmake_minimum_required(VERSION 3.20)
project(msgfmt LANGUAGES CXX)

option(MSGFMT_WERROR "Treat compiler warnings as errors" OFF)
option(MSGFMT_SANITIZE "Build with AddressSanitizer and UndefinedBehaviorSanitizer" OFF)

set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Warnings are attached to our own targets only, so flags passed by whoever
# builds the project (e.g. CMAKE_CXX_FLAGS) are left untouched.
add_library(msgfmt_warnings INTERFACE)
target_compile_options(msgfmt_warnings INTERFACE
    -Wall -Wextra -Wpedantic -Wshadow -Wconversion -Wsign-conversion -Wold-style-cast)
if(MSGFMT_WERROR)
    # Off by default: a newer compiler may add a warning we have never seen,
    # and that must not break someone else's build.
    target_compile_options(msgfmt_warnings INTERFACE -Werror)
endif()

if(MSGFMT_SANITIZE)
    # -fno-sanitize-recover makes UBSan abort, so ctest sees a failure
    # instead of a message on stderr and exit code 0.
    add_compile_options(-fsanitize=address,undefined -fno-sanitize-recover=all
                        -fno-omit-frame-pointer)
    add_link_options(-fsanitize=address,undefined)
endif()

add_library(msgfmt_lib
    src/escape.cpp
    src/json.cpp
    src/parser.cpp
    src/style.cpp
    src/unicode.cpp
    src/utf8.cpp
)
target_include_directories(msgfmt_lib
    PUBLIC include
    PRIVATE src
)
target_compile_features(msgfmt_lib PUBLIC cxx_std_17)
target_link_libraries(msgfmt_lib PRIVATE msgfmt_warnings)

add_executable(msgfmt src/main.cpp)
target_link_libraries(msgfmt PRIVATE msgfmt_lib msgfmt_warnings)

include(CTest)
if(BUILD_TESTING)
    add_subdirectory(tests)
endif()
