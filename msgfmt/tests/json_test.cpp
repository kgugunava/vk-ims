// JSON serialization of a parsed message: the exact output format, field set
// and string escaping.

#include "json.hpp"

#include <string>

#include "msgfmt/msgfmt.hpp"
#include "test_util.hpp"

namespace {

using msgfmt::Message;
using msgfmt::Range;
using msgfmt::Style;
using msgfmt::detail::InvalidUtf8Json;
using msgfmt::detail::ToJson;
using msgfmt::test::Bytes;
using msgfmt::test::Checker;
using msgfmt::test::Describe;

struct Case {
    const char* name;
    Message message;
    std::string want;
};

void TestMessages(Checker& check) {
    const Case cases[] = {
        {"empty message", Message{}, R"({"text":"","ranges":[]})"},
        {"example from the task", Message{"Привет, мир!", {{Style::kBold, 8, 3, ""}}},
         R"({"text":"Привет, мир!","ranges":[{"style":"bold","offset":8,"length":3,"url":""}]})"},
        {"all styles, comma-separated",
         Message{"abcd",
                 {{Style::kBold, 0, 1, ""},
                  {Style::kItalic, 1, 1, ""},
                  {Style::kCode, 2, 1, ""},
                  {Style::kLink, 3, 1, "https://x.ru/?a=1&b=2"}}},
         R"({"text":"abcd","ranges":[)"
         R"({"style":"bold","offset":0,"length":1,"url":""},)"
         R"({"style":"italic","offset":1,"length":1,"url":""},)"
         R"({"style":"code","offset":2,"length":1,"url":""},)"
         R"({"style":"link","offset":3,"length":1,"url":"https://x.ru/?a=1&b=2"}]})"},
        {"url is escaped", Message{"a", {{Style::kLink, 0, 1, "u\\\"v"}}},
         R"({"text":"a","ranges":[{"style":"link","offset":0,"length":1,"url":"u\\\"v"}]})"},
        {"large offsets", Message{"", {{Style::kBold, 1234567, 89, ""}}},
         R"({"text":"","ranges":[{"style":"bold","offset":1234567,"length":89,"url":""}]})"},
    };
    for (const Case& c : cases) {
        const std::string got = ToJson(c.message);
        check.Expect(got == c.want, std::string(c.name) + ": ToJson(" + Describe(c.message) +
                                        ")\n  got:  " + got + "\n  want: " + c.want);
    }
}

void TestStringEscaping(Checker& check) {
    struct EscapeCase {
        const char* name;
        std::string text;
        std::string want;  // the JSON string literal, quotes included
    };
    const EscapeCase cases[] = {
        {"quote", "a\"b", R"("a\"b")"},
        {"backslash", "a\\b", R"("a\\b")"},
        {"newline", "a\nb", R"("a\nb")"},
        {"short escapes", "\b\f\r\t", R"("\b\f\r\t")"},
        {"NUL", Bytes({0x61, 0x00, 0x62}), R"("a\u0000b")"},
        {"other controls", Bytes({0x01, 0x1F}), R"("\u0001\u001f")"},
        {"DEL is not escaped", Bytes({0x7F}), "\"\x7F\""},
        {"slash is not escaped", "a/b", R"("a/b")"},
        {"Cyrillic and emoji as is", "я😀", "\"я😀\""},
        {"line separator as is", " ", "\" \""},
    };
    for (const EscapeCase& c : cases) {
        const std::string got = ToJson(Message{c.text, {}});
        const std::string want = R"({"text":)" + c.want + R"(,"ranges":[]})";
        check.Expect(got == want, std::string(c.name) + "\n  got:  " + got + "\n  want: " + want);
    }
}

void TestErrorObject(Checker& check) {
    check.Expect(InvalidUtf8Json() == R"({"error":"invalid_utf8"})",
                 "InvalidUtf8Json() = " + InvalidUtf8Json());
}

}  // namespace

int main() {
    Checker check;
    TestMessages(check);
    TestStringEscaping(check);
    TestErrorObject(check);
    return check.ExitCode();
}
