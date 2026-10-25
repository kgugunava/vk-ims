// Parse() against the rules of the markup. Each case is named after the rule
// or appendix example it checks; expectations are written by hand from the
// rules, not taken from the implementation.

#include <optional>
#include <string>

#include "msgfmt/msgfmt.hpp"
#include "test_util.hpp"

namespace {

using msgfmt::Message;
using msgfmt::Range;
using msgfmt::Style;
using msgfmt::test::Bytes;
using msgfmt::test::Checker;
using msgfmt::test::Describe;
using msgfmt::test::Quote;

struct Case {
    const char* name;
    std::string input;
    std::optional<Message> want;  // std::nullopt: input is not valid UTF-8
};

Range Bold(std::size_t offset, std::size_t length) { return {Style::kBold, offset, length, ""}; }
Range Italic(std::size_t offset, std::size_t length) {
    return {Style::kItalic, offset, length, ""};
}

/// Input that must come out unchanged and without ranges.
Case Literal(const char* name, std::string input) {
    Message want{input, {}};
    return {name, std::move(input), std::move(want)};
}

void Run(Checker& check, const Case& c) {
    const std::optional<Message> got = msgfmt::Parse(c.input);
    check.Expect(got == c.want, std::string(c.name) + ": Parse(" + Quote(c.input) + ")\n  got:  " +
                                    Describe(got) + "\n  want: " + Describe(c.want));
}

void TestAppendixA(Checker& check) {
    const Case cases[] = {
        {"A1", "**важно**", Message{"важно", {Bold(0, 5)}}},
        {"A2", "Привет, **мир**!", Message{"Привет, мир!", {Bold(8, 3)}}},
        {"A3", "_курсив_", Message{"курсив", {Italic(0, 6)}}},
        // The underscores are not at word boundaries.
        Literal("A4", "a_b_c"),
        Literal("A5", "**не закрыто"),
        {"A6", "2 \\* 3", Message{"2 * 3", {}}},
        // The emoji takes two UTF-16 code units, plus one for the space.
        {"A10", "💅 **ок**", Message{"💅 ок", {Bold(3, 2)}}},
        {"A11", "**💅 ок**", Message{"💅 ок", {Bold(0, 5)}}},
        {"A13", "\\\\", Message{"\\", {}}},
        {"A14", "", Message{}},
        Literal("A15", "_ e_"),
    };
    for (const Case& c : cases) {
        Run(check, c);
    }
}

void TestBold(Checker& check) {
    const Case cases[] = {
        // Rule 2.
        {"bold", "**a**", Message{"a", {Bold(0, 1)}}},
        {"bold needs no word boundary", "a**b**c", Message{"abc", {Bold(1, 1)}}},
        {"two bold ranges", "**a** **b**", Message{"a b", {Bold(0, 1), Bold(2, 1)}}},
        {"whitespace inside is fine", "**a b**", Message{"a b", {Bold(0, 3)}}},
        {"newline inside is fine", "**a\nb**", Message{"a\nb", {Bold(0, 3)}}},

        // Rule 4: an opener must be followed by non-whitespace.
        Literal("space after opener", "2 ** 3"),
        Literal("space after opener, closer later", "** 3**"),
        Literal("newline after opener", "**\n**"),
        Literal("no-break space after opener", "** a**"),
        Literal("opener at the very end", "a **"),
        Literal("just the marker", "**"),

        // Rule 5: the nearest closer preceded by non-whitespace.
        {"skips a closer after a space", "**a **b**", Message{"a **b", {Bold(0, 5)}}},
        {"takes the nearest closer", "**a** b**", Message{"a b**", {Bold(0, 1)}}},
        Literal("only closer follows a space", "**a **"),
        Literal("only closer follows a no-break space", "**a **"),
        {"three asterisks around", "***a***", Message{"*a*", {Bold(0, 2)}}},
        {"three asterisks at the end", "**a***", Message{"a*", {Bold(0, 1)}}},

        // Rule 6 and D4: no closer, or nothing between the markers.
        Literal("unclosed", "**a"),
        Literal("empty content", "****"),
        {"lone asterisk as content", "*****", Message{"*", {Bold(0, 1)}}},

        // D16: a single asterisk is not a marker.
        Literal("single asterisks", "*a*"),
        Literal("single asterisk", "2 * 3"),
    };
    for (const Case& c : cases) {
        Run(check, c);
    }
}

void TestItalic(Checker& check) {
    const Case cases[] = {
        // Rule 3, opening `_`: start of text or a non-letter, non-digit before it.
        {"italic", "_b_", Message{"b", {Italic(0, 1)}}},
        {"after a space", "x _a_ y", Message{"x a y", {Italic(2, 1)}}},
        {"inside parentheses", "(_a_)", Message{"(a)", {Italic(1, 1)}}},
        {"after an asterisk", "*_a_", Message{"*a", {Italic(1, 1)}}},
        {"after a no-break space", " _a_", Message{" a", {Italic(1, 1)}}},
        {"after an emoji", "😀_a_", Message{"😀a", {Italic(2, 1)}}},
        Literal("after a Latin letter", "x_a_"),
        Literal("after a Cyrillic letter", "я_б_"),
        Literal("after a digit", "1_a_"),
        Literal("snake_case in a sentence", "use snake_case_name here"),
        // D9: a combining mark is not a letter, so a decomposed letter does
        // not block the opener while its precomposed form does.
        {"after a decomposed short i", "й_a_", Message{"йa", {Italic(2, 1)}}},
        Literal("after a precomposed short i", "й_a_"),

        // Rule 3, closing `_`: end of text or a non-letter, non-digit after it.
        {"before a comma", "_a_,", Message{"a,", {Italic(0, 1)}}},
        {"before an asterisk", "_a_*", Message{"a*", {Italic(0, 1)}}},
        {"before an emoji", "_a_😀", Message{"a😀", {Italic(0, 1)}}},
        Literal("before a Latin letter", "_a_b"),
        Literal("before a Cyrillic letter", "_a_я"),
        Literal("before a digit", "_a_1"),
        {"inner underscore of a word is skipped", "_a_b_", Message{"a_b", {Italic(0, 3)}}},

        // Rule 4: non-whitespace after the opener.
        Literal("space after opener", "_ a_"),
        Literal("newline after opener", "_\na_"),
        Literal("no-break space after opener", "_ a_"),

        // Rule 5: nearest closer preceded by non-whitespace.
        Literal("only closer follows a space", "_a _"),
        {"skips a closer after a space", "_a _b_", Message{"a _b", {Italic(0, 4)}}},
        {"two italic ranges", "_a_ _b_", Message{"a b", {Italic(0, 1), Italic(2, 1)}}},

        // Rule 6 and D4.
        Literal("unclosed", "_a"),
        Literal("empty content", "__"),
        {"lone underscore as content", "___", Message{"_", {Italic(0, 1)}}},
        // The closer `_` is followed by another `_`, which is not a letter;
        // the inner `_` is text because italic is already open (rule 10).
        {"dunder name", "__init__", Message{"_init_", {Italic(0, 5)}}},

        // Rule 9 inside italic.
        {"escaped underscore inside", "_a\\_b_", Message{"a_b", {Italic(0, 3)}}},
        {"escaped closer", "_a\\_", Message{"_a_", {}}},
    };
    for (const Case& c : cases) {
        Run(check, c);
    }
}

void TestEscapes(Checker& check) {
    const Case cases[] = {
        // Rule 9.
        {"escaped backslash", "\\\\", Message{"\\", {}}},
        Literal("backslash before a letter is kept", "\\a"),
        Literal("lone backslash", "\\"),
        Literal("trailing backslash", "a\\"),
        {"escaped opener", "\\**a**", Message{"**a**", {}}},
        {"escaped underscore", "\\_a_", Message{"_a_", {}}},
        {"escaped link brackets", "\\[a](u)", Message{"[a](u)", {}}},
        {"escaped backticks", "\\`a\\`", Message{"`a`", {}}},
        {"escaped parentheses", "\\(a\\)", Message{"(a)", {}}},
        {"escape inside bold", "**a\\*b**", Message{"a*b", {Bold(0, 3)}}},
        // The first '*' of the would-be closer is escaped, so there is no pair.
        {"escaped half of the closer", "**a\\**", Message{"**a**", {}}},
        {"escaped backslash before bold", "\\\\**a**", Message{"\\a", {Bold(1, 1)}}},
        {"escaped backslash before closer", "**a\\\\**", Message{"a\\", {Bold(0, 2)}}},
    };
    for (const Case& c : cases) {
        Run(check, c);
    }
}

void TestUtf16Offsets(Checker& check) {
    const Case cases[] = {
        {"emoji is two units", "**😀**", Message{"😀", {Bold(0, 2)}}},
        {"emoji before bold", "💅**ок**", Message{"💅ок", {Bold(2, 2)}}},
        {"precomposed short i", "**й**", Message{"й", {Bold(0, 1)}}},
        // No normalization: i + combining breve stays two code points.
        {"decomposed short i", "**й**", Message{"й", {Bold(0, 2)}}},
        {"last BMP code point", "**￿**", Message{"￿", {Bold(0, 1)}}},
        {"first supplementary code point", "**\U00010000**", Message{"\U00010000", {Bold(0, 2)}}},
        {"offset after supplementary", "\U00010000\U00010000 **a**",
         Message{"\U00010000\U00010000 a", {Bold(5, 1)}}},
    };
    for (const Case& c : cases) {
        Run(check, c);
    }
}

void TestInputHandling(Checker& check) {
    const Case cases[] = {
        Literal("NUL is an ordinary character", Bytes({0x61, 0x00, 0x62})),
        Literal("control characters are text", "a\tb\r\n"),
        {"invalid UTF-8", Bytes({0xFF}), std::nullopt},
        {"invalid UTF-8 after markup", "**a**" + Bytes({0xC3}), std::nullopt},
    };
    for (const Case& c : cases) {
        Run(check, c);
    }
}

}  // namespace

int main() {
    Checker check;
    TestAppendixA(check);
    TestBold(check);
    TestItalic(check);
    TestEscapes(check);
    TestUtf16Offsets(check);
    TestInputHandling(check);
    return check.ExitCode();
}
