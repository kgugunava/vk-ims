# msgfmt_add_test(<name>) builds tests/<name>_test.cpp and registers it in
# CTest as msgfmt.<name>. Tests may include internal headers from src/.
function(msgfmt_add_test name)
    add_executable(${name}_test ${name}_test.cpp)
    target_include_directories(${name}_test PRIVATE ${PROJECT_SOURCE_DIR}/src)
    target_link_libraries(${name}_test PRIVATE msgfmt_lib msgfmt_warnings)
    add_test(NAME msgfmt.${name} COMMAND ${name}_test)
endfunction()

msgfmt_add_test(api)

# Guards the runner itself: a failed check must turn into a non-zero exit
# code, otherwise every other test could pass while checking nothing.
msgfmt_add_test(runner_selftest)
set_tests_properties(msgfmt.runner_selftest PROPERTIES WILL_FAIL TRUE)
