// Escape(): a backslash before every special character, and the guarantee
// that makes it useful: parsing the escaped text gives the original text back
// with no ranges.

#include <optional>
#include <string>

#include "msgfmt/msgfmt.hpp"
#include "test_util.hpp"

namespace {

using msgfmt::Message;
using msgfmt::test::Bytes;
using msgfmt::test::Checker;
using msgfmt::test::Describe;
using msgfmt::test::HexBytes;
using msgfmt::test::Quote;

void TestEscape(Checker& check) {
    struct Case {
        const char* name;
        std::string input;
        std::optional<std::string> want;
    };
    const Case cases[] = {
        {"example from the task", "2 * 3", "2 \\* 3"},
        {"every special character", "*_`[]()\\", "\\*\\_\\`\\[\\]\\(\\)\\\\"},
        {"markup", "**a** [b](u)", "\\*\\*a\\*\\* \\[b\\]\\(u\\)"},
        {"no special characters", "привет 😀", "привет 😀"},
        {"other punctuation is kept", "a-b.c!{}<>#", "a-b.c!{}<>#"},
        {"empty input", "", ""},
        {"invalid UTF-8", Bytes({0x61, 0xFF}), std::nullopt},
        {"truncated sequence", Bytes({0xC3}), std::nullopt},
    };
    for (const Case& c : cases) {
        const std::optional<std::string> got = msgfmt::Escape(c.input);
        check.Expect(got == c.want, std::string(c.name) + ": Escape(" + Quote(c.input) +
                                        ") = " + (got ? Quote(*got) : "nullopt") + ", want " +
                                        (c.want ? Quote(*c.want) : "nullopt"));
    }
}

void TestRoundTrip(Checker& check) {
    const std::string inputs[] = {
        "",
        "plain text",
        "**a**",
        "_a_",
        "`a`",
        "[a](u)",
        "**_`[a](u)`_**",
        "\\",
        "\\\\",
        "a\\b",
        "\\*",
        "ends with a backslash\\",
        "привет 😀 **мир**",
        Bytes({0x61, 0x00, 0x2A, 0x2A}),  // a NUL * *
    };
    for (const std::string& input : inputs) {
        const std::optional<std::string> escaped = msgfmt::Escape(input);
        if (!escaped) {
            check.Expect(false, "Escape(" + HexBytes(input) + ") rejected valid UTF-8");
            continue;
        }
        const std::optional<Message> parsed = msgfmt::Parse(*escaped);
        check.Expect(parsed == Message{input, {}}, "Parse(Escape(" + Quote(input) +
                                                       ")) = " + Describe(parsed) +
                                                       ", want the input as text");
    }
}

}  // namespace

int main() {
    Checker check;
    TestEscape(check);
    TestRoundTrip(check);
    return check.ExitCode();
}
