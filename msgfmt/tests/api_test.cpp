// Checks of the public types that do not depend on the parser: style names
// used in JSON, field-wise equality, and the empty-input result.

#include <string>

#include "msgfmt/msgfmt.hpp"
#include "test_util.hpp"

namespace {

using msgfmt::Message;
using msgfmt::Range;
using msgfmt::Style;
using msgfmt::test::Checker;
using msgfmt::test::Describe;

void TestStyleNames(Checker& check) {
    check.Expect(msgfmt::StyleName(Style::kBold) == "bold", "StyleName(kBold)");
    check.Expect(msgfmt::StyleName(Style::kItalic) == "italic", "StyleName(kItalic)");
    check.Expect(msgfmt::StyleName(Style::kCode) == "code", "StyleName(kCode)");
    check.Expect(msgfmt::StyleName(Style::kLink) == "link", "StyleName(kLink)");
}

void TestRangeEquality(Checker& check) {
    const Range base{Style::kLink, 1, 2, "u"};
    check.Expect(base == Range{Style::kLink, 1, 2, "u"}, "equal ranges compare equal");

    // Each field on its own must break equality.
    const Range variants[] = {
        {Style::kBold, 1, 2, "u"},
        {Style::kLink, 0, 2, "u"},
        {Style::kLink, 1, 3, "u"},
        {Style::kLink, 1, 2, "v"},
    };
    for (const Range& other : variants) {
        check.Expect(base != other, Describe(base) + " != " + Describe(other));
    }
}

void TestMessageEquality(Checker& check) {
    const Message base{"ab", {{Style::kBold, 0, 1, ""}}};
    check.Expect(base == Message{"ab", {{Style::kBold, 0, 1, ""}}}, "equal messages compare equal");
    check.Expect(base != Message{"ac", {{Style::kBold, 0, 1, ""}}}, "text differs");
    check.Expect(base != Message{"ab", {}}, "ranges differ");
}

void TestEmptyInput(Checker& check) {
    // Rule 11: empty input gives empty text and no ranges.
    const auto got = msgfmt::Parse("");
    check.Expect(got == Message{},
                 "Parse(\"\") = " + Describe(got) + ", want {text: \"\", ranges: []}");
}

}  // namespace

int main() {
    Checker check;
    TestStyleNames(check);
    TestRangeEquality(check);
    TestMessageEquality(check);
    TestEmptyInput(check);
    return check.ExitCode();
}
