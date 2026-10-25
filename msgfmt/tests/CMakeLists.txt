# GCC's AddressSanitizer checks stack use after return by default, which costs
# about 10 us per function call: tests that make millions of calls would run
# for minutes. Clang leaves the check off by default; tests do the same unless
# this is overridden. scripts/check-msgfmt.sh runs the non-heavy tests with the
# check on, so it is not lost. Has no effect on builds without sanitizers.
set(MSGFMT_TEST_ASAN_OPTIONS "detect_stack_use_after_return=0"
    CACHE STRING "ASAN_OPTIONS passed to every msgfmt test")

# msgfmt_add_test(<name>) builds tests/<name>_test.cpp and registers it in
# CTest as msgfmt.<name>. Tests may include internal headers from src/.
function(msgfmt_add_test name)
    add_executable(${name}_test ${name}_test.cpp)
    target_include_directories(${name}_test PRIVATE ${PROJECT_SOURCE_DIR}/src)
    target_link_libraries(${name}_test PRIVATE msgfmt_lib msgfmt_warnings)
    add_test(NAME msgfmt.${name} COMMAND ${name}_test)
    set_tests_properties(msgfmt.${name} PROPERTIES
        ENVIRONMENT "ASAN_OPTIONS=${MSGFMT_TEST_ASAN_OPTIONS}")
endfunction()

msgfmt_add_test(api)
msgfmt_add_test(escape)
msgfmt_add_test(json)
msgfmt_add_test(parser)
msgfmt_add_test(unicode)
msgfmt_add_test(utf8)

# Tests that make millions of calls are labelled heavy, so slow sanitizer
# configurations can skip them with `ctest -LE heavy`.
set_tests_properties(msgfmt.utf8 PROPERTIES LABELS heavy)

# msgfmt_add_cli_test(<name> <exit code> [args...]) runs the msgfmt binary with
# cli/<name>.in on stdin and compares its stdout with cli/<name>.out byte for
# byte, and its exit code with <exit code>.
function(msgfmt_add_cli_test name expected_code)
    string(JOIN " " args ${ARGN})
    add_test(NAME msgfmt.cli.${name}
        COMMAND ${CMAKE_COMMAND}
            -DBIN=$<TARGET_FILE:msgfmt>
            "-DARGS=${args}"
            -DINPUT=${CMAKE_CURRENT_SOURCE_DIR}/cli/${name}.in
            -DEXPECTED=${CMAKE_CURRENT_SOURCE_DIR}/cli/${name}.out
            -DEXPECTED_CODE=${expected_code}
            -P ${CMAKE_CURRENT_SOURCE_DIR}/run_cli.cmake)
    set_tests_properties(msgfmt.cli.${name} PROPERTIES
        ENVIRONMENT "ASAN_OPTIONS=${MSGFMT_TEST_ASAN_OPTIONS}")
endfunction()

msgfmt_add_cli_test(parse_example 0)
msgfmt_add_cli_test(parse_empty 0)
msgfmt_add_cli_test(parse_json_escaping 0)
msgfmt_add_cli_test(parse_invalid_utf8 2)
msgfmt_add_cli_test(escape_example 0 escape)
msgfmt_add_cli_test(escape_all_specials 0 escape)
msgfmt_add_cli_test(escape_keeps_unicode 0 escape)
msgfmt_add_cli_test(escape_empty 0 escape)
msgfmt_add_cli_test(escape_invalid_utf8 2 escape)
msgfmt_add_cli_test(unknown_command 1 foo)
msgfmt_add_cli_test(extra_argument 1 escape x)

# Guards the runner itself: a failed check must turn into a non-zero exit
# code, otherwise every other test could pass while checking nothing.
msgfmt_add_test(runner_selftest)
set_tests_properties(msgfmt.runner_selftest PROPERTIES WILL_FAIL TRUE)
