// Registered with WILL_FAIL: passes only if a failed check makes the test
// binary exit with a non-zero code.

#include "test_util.hpp"

int main() {
    msgfmt::test::Checker check;
    check.Expect(true, "a passing check does not count as a failure");
    check.Expect(false, "deliberate failure");
    return check.ExitCode();
}
