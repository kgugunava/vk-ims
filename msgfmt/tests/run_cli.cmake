# Runs the msgfmt binary as a black box; invoked by CTest via `cmake -P`.
#
#   BIN            path to msgfmt
#   ARGS           command-line arguments, space-separated (may be empty)
#   INPUT          file fed to stdin
#   EXPECTED       file holding the exact expected stdout bytes
#   EXPECTED_CODE  expected exit code
#
# stderr must be empty when the expected exit code is 0.
cmake_minimum_required(VERSION 3.20)

separate_arguments(arg_list UNIX_COMMAND "${ARGS}")
execute_process(
    COMMAND "${BIN}" ${arg_list}
    INPUT_FILE "${INPUT}"
    OUTPUT_VARIABLE out
    ERROR_VARIABLE err
    RESULT_VARIABLE code
)
file(READ "${EXPECTED}" want)

set(problems "")
if(NOT code STREQUAL EXPECTED_CODE)
    string(APPEND problems "exit code: got ${code}, want ${EXPECTED_CODE}\n")
endif()
if(NOT out STREQUAL want)
    string(APPEND problems "stdout:\n  got:  [${out}]\n  want: [${want}]\n")
endif()
if(EXPECTED_CODE STREQUAL "0" AND NOT err STREQUAL "")
    string(APPEND problems "stderr is not empty: [${err}]\n")
endif()

if(NOT problems STREQUAL "")
    message(FATAL_ERROR "msgfmt ${ARGS} < ${INPUT}\n${problems}stderr: [${err}]")
endif()
