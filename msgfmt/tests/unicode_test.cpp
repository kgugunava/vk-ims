// Character classes behind the parsing rules: "letter or digit" (rule 3,
// word boundaries of `_`) and "whitespace" (rules 3-5).

#include "unicode.hpp"

#include <cstdint>
#include <string>

#include "test_util.hpp"

namespace {

using msgfmt::detail::IsLetterOrDigit;
using msgfmt::detail::IsWhitespace;
using msgfmt::test::Checker;
using msgfmt::test::Hex;

struct Case {
    char32_t cp;
    const char* name;
};

std::string Name(const Case& c) {
    return "U+" + Hex(static_cast<std::uint32_t>(c.cp), 4) + " (" + c.name + ")";
}

void TestLetterOrDigit(Checker& check) {
    const Case letters_and_digits[] = {
        {U'a', "Latin small a"},
        {U'Z', "Latin capital Z"},
        {U'0', "digit zero"},
        {U'9', "digit nine"},
        {0x044F, "Cyrillic ya"},
        {0x0401, "Cyrillic capital Yo"},
        {0x00DF, "sharp s, Ll"},
        {0x6F22, "CJK ideograph, Lo"},
        {0x0663, "Arabic-Indic digit three, Nd"},
        {0x01C5, "titlecase Dz with caron, Lt"},
        {0x02B0, "modifier small h, Lm"},
        {0x323AF, "last code point of the table"},
    };
    for (const Case& c : letters_and_digits) {
        check.Expect(IsLetterOrDigit(c.cp), Name(c) + " should be a letter or digit");
    }

    const Case others[] = {
        {U'_', "low line"},
        {U'*', "asterisk"},
        {U' ', "space"},
        {U'-', "hyphen-minus"},
        {U',', "comma"},
        {U'/', "just below '0'"},
        {U':', "just above '9'"},
        {U'`', "just below 'a'"},
        {U'[', "just above 'Z'"},
        {0x0306, "combining breve, Mn: decomposed letters end with it"},
        {0x00B2, "superscript two, No"},
        {0x216B, "Roman numeral twelve, Nl"},
        {0x1F600, "emoji"},
        {0x200B, "zero width space"},
        {0x323B0, "first code point after the table"},
        {0x10FFFF, "last code point"},
    };
    for (const Case& c : others) {
        check.Expect(!IsLetterOrDigit(c.cp), Name(c) + " should not be a letter or digit");
    }
}

void TestWhitespace(Checker& check) {
    // The full White_Space list, written out independently of the table in
    // unicode.cpp.
    const char32_t white_space[] = {
        0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x0085, 0x00A0, 0x1680,
        0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008,
        0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000,
    };
    for (const char32_t cp : white_space) {
        check.Expect(IsWhitespace(cp),
                     "U+" + Hex(static_cast<std::uint32_t>(cp), 4) + " should be whitespace");
    }

    const Case others[] = {
        {U'a', "letter"},
        {U'_', "low line"},
        {0x0008, "backspace, just below tab"},
        {0x000E, "just above carriage return"},
        {0x001C, "file separator: str.isspace() accepts it, White_Space does not"},
        {0x001F, "unit separator"},
        {0x180E, "Mongolian vowel separator, removed from White_Space in Unicode 6.3"},
        {0x200B, "zero width space"},
        {0xFEFF, "BOM"},
    };
    for (const Case& c : others) {
        check.Expect(!IsWhitespace(c.cp), Name(c) + " should not be whitespace");
    }
}

}  // namespace

int main() {
    Checker check;
    TestLetterOrDigit(check);
    TestWhitespace(check);
    return check.ExitCode();
}
