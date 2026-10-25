// Inputs of about 1 MiB built to defeat a naive closing-marker search: many
// openers with no closer, so each one would scan to the end of the input.
// The checks are on the result; running time is bounded by the CTest TIMEOUT
// of this test, not measured here, so the test stays deterministic. A
// quadratic parser would take hours on these inputs and hit the timeout.

#include <chrono>
#include <cstddef>
#include <iostream>
#include <optional>
#include <string>

#include "msgfmt/msgfmt.hpp"
#include "test_util.hpp"

namespace {

using msgfmt::Message;
using msgfmt::test::Checker;

constexpr std::size_t kTargetBytes = 1 << 20;

std::string Repeat(const std::string& unit, std::size_t total_bytes = kTargetBytes) {
    std::string out;
    out.reserve(total_bytes + unit.size());
    while (out.size() < total_bytes) {
        out += unit;
    }
    return out;
}

struct Case {
    const char* name;
    std::string input;
    std::string want_text;
    std::size_t want_ranges;
};

void Run(Checker& check, const Case& c) {
    const auto start = std::chrono::steady_clock::now();
    const std::optional<Message> got = msgfmt::Parse(c.input);
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now() - start);
    // Informational only; see the comment at the top.
    std::cout << c.name << ": " << elapsed.count() << " ms\n";

    if (!got) {
        check.Expect(false, std::string(c.name) + ": rejected as invalid UTF-8");
        return;
    }
    check.Expect(got->text == c.want_text, std::string(c.name) + ": text differs (" +
                                               std::to_string(got->text.size()) + " bytes, want " +
                                               std::to_string(c.want_text.size()) + ")");
    check.Expect(got->ranges.size() == c.want_ranges,
                 std::string(c.name) + ": " + std::to_string(got->ranges.size()) +
                     " ranges, want " + std::to_string(c.want_ranges));
}

}  // namespace

int main() {
    // Every opener below is followed by a space-preceded marker or by nothing,
    // so no closer exists and the text comes out unchanged.
    const std::string bold_openers = Repeat("**a ");
    const std::string italic_openers = Repeat("_a ");
    const std::string code_openers = Repeat("`a ");
    const std::string link_openers = Repeat("[a");
    const std::string bold_then_text = "**" + Repeat("a");
    const std::string unclosed_url = "[a](" + Repeat("x");
    const std::string mixed_openers = Repeat("**a _a `a [a ");

    // Many short closed ranges: the cost is in emitting and sorting them.
    const std::string closed_bold = Repeat("**a** ");
    const std::size_t closed_bold_count = closed_bold.size() / 6;

    // Backslashes escape each other in pairs.
    const std::string backslashes = Repeat("\\\\");

    const Case cases[] = {
        {"bold openers", bold_openers, bold_openers, 0},
        {"italic openers", italic_openers, italic_openers, 0},
        {"code openers", code_openers, code_openers, 0},
        {"link openers", link_openers, link_openers, 0},
        {"mixed openers", mixed_openers, mixed_openers, 0},
        {"one bold opener, long text", bold_then_text, bold_then_text, 0},
        {"unclosed url", unclosed_url, unclosed_url, 0},
        {"many closed bold ranges", closed_bold, Repeat("a ", closed_bold_count * 2),
         closed_bold_count},
        {"backslashes", backslashes, std::string(backslashes.size() / 2, '\\'), 0},
    };

    Checker check;
    for (const Case& c : cases) {
        Run(check, c);
    }
    return check.ExitCode();
}
