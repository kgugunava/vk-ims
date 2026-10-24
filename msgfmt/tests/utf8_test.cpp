// UTF-8 decoding and encoding, and UTF-16 lengths: the base of every offset
// msgfmt reports.

#include "utf8.hpp"

#include <cstdint>
#include <string>

#include "test_util.hpp"

namespace {

using msgfmt::detail::AppendUtf8;
using msgfmt::detail::DecodeUtf8;
using msgfmt::detail::Utf16Length;
using msgfmt::test::Bytes;
using msgfmt::test::Checker;
using msgfmt::test::Describe;
using msgfmt::test::Hex;
using msgfmt::test::HexBytes;

struct ValidCase {
    const char* name;
    std::string bytes;
    std::u32string code_points;
};

struct InvalidCase {
    const char* name;
    std::string bytes;
};

void TestValidSequences(Checker& check) {
    const ValidCase cases[] = {
        {"U+0000 is an ordinary character", Bytes({0x00}), {0x0000}},
        {"last 1-byte", Bytes({0x7F}), {0x007F}},
        {"first 2-byte", Bytes({0xC2, 0x80}), {0x0080}},
        {"last 2-byte", Bytes({0xDF, 0xBF}), {0x07FF}},
        {"first 3-byte", Bytes({0xE0, 0xA0, 0x80}), {0x0800}},
        {"last before surrogates", Bytes({0xED, 0x9F, 0xBF}), {0xD7FF}},
        {"first after surrogates", Bytes({0xEE, 0x80, 0x80}), {0xE000}},
        {"BOM is an ordinary character", Bytes({0xEF, 0xBB, 0xBF}), {0xFEFF}},
        {"last 3-byte", Bytes({0xEF, 0xBF, 0xBF}), {0xFFFF}},
        {"first 4-byte", Bytes({0xF0, 0x90, 0x80, 0x80}), {0x10000}},
        {"last code point", Bytes({0xF4, 0x8F, 0xBF, 0xBF}), {0x10FFFF}},
        {"mixed: a, ya, emoji",
         Bytes({0x61, 0xD1, 0x8F, 0xF0, 0x9F, 0x98, 0x80}),
         {0x0061, 0x044F, 0x1F600}},
        {"empty input", "", {}},
    };

    for (const ValidCase& c : cases) {
        const auto decoded = DecodeUtf8(c.bytes);
        check.Expect(decoded == c.code_points, std::string(c.name) + ": DecodeUtf8(" +
                                                   HexBytes(c.bytes) + ") = " + Describe(decoded) +
                                                   ", want " + Describe(c.code_points));

        std::string encoded;
        for (const char32_t cp : c.code_points) {
            AppendUtf8(encoded, cp);
        }
        check.Expect(encoded == c.bytes, std::string(c.name) + ": AppendUtf8(" +
                                             Describe(c.code_points) + ") = " + HexBytes(encoded) +
                                             ", want " + HexBytes(c.bytes));
    }
}

void TestInvalidSequences(Checker& check) {
    const InvalidCase cases[] = {
        {"lone continuation byte 80", Bytes({0x80})},
        {"lone continuation byte BF", Bytes({0xBF})},
        {"truncated 2-byte", Bytes({0xC3})},
        {"2-byte lead before ASCII", Bytes({0xC3, 0x28})},
        {"overlong '/' via C0", Bytes({0xC0, 0xAF})},
        {"overlong via C1", Bytes({0xC1, 0xBF})},
        {"overlong 3-byte", Bytes({0xE0, 0x80, 0xAF})},
        {"overlong U+07FF as 3 bytes", Bytes({0xE0, 0x9F, 0xBF})},
        {"surrogate U+D800", Bytes({0xED, 0xA0, 0x80})},
        {"surrogate U+DFFF", Bytes({0xED, 0xBF, 0xBF})},
        {"overlong 4-byte", Bytes({0xF0, 0x8F, 0xBF, 0xBF})},
        {"above U+10FFFF", Bytes({0xF4, 0x90, 0x80, 0x80})},
        {"lead F5", Bytes({0xF5, 0x80, 0x80, 0x80})},
        {"5-byte form", Bytes({0xF8, 0x88, 0x80, 0x80, 0x80})},
        {"byte FE", Bytes({0xFE})},
        {"byte FF", Bytes({0xFF})},
        {"truncated emoji", Bytes({0xF0, 0x9F, 0x98})},
        {"truncated 3-byte", Bytes({0xE2, 0x82})},
        {"bad continuation in 3-byte", Bytes({0xE2, 0x28, 0xA1})},
        {"bad last byte in 4-byte", Bytes({0xF0, 0x9F, 0x98, 0x28})},
        {"error in the middle", Bytes({0x61, 0xFF, 0x62})},
        {"truncated at the end", Bytes({0x61, 0xC3})},
    };

    for (const InvalidCase& c : cases) {
        const auto decoded = DecodeUtf8(c.bytes);
        check.Expect(!decoded, std::string(c.name) + ": DecodeUtf8(" + HexBytes(c.bytes) +
                                   ") = " + Describe(decoded) + ", want nullopt");
    }
}

bool IsSurrogate(std::uint32_t value) { return value >= 0xD800 && value <= 0xDFFF; }

/// Every scalar value must be encoded in its shortest form and decode back to
/// itself. All of them go through one string and one DecodeUtf8 call: a call
/// per code point takes minutes under GCC's AddressSanitizer, which checks
/// stack use after return by default and pays for it on every call.
void TestAllScalarValues(Checker& check) {
    std::string all;
    std::u32string expected;
    for (std::uint32_t value = 0; value <= 0x10FFFF; ++value) {
        if (IsSurrogate(value)) {
            continue;
        }
        const auto cp = static_cast<char32_t>(value);
        const std::size_t before = all.size();
        AppendUtf8(all, cp);
        const std::size_t want_length = value < 0x80      ? 1
                                        : value < 0x800   ? 2
                                        : value < 0x10000 ? 3
                                                          : 4;
        if (all.size() - before != want_length) {
            check.Expect(false, "AppendUtf8(U+" + Hex(value, 4) +
                                    ") = " + HexBytes(std::string_view(all).substr(before)) +
                                    ", want " + std::to_string(want_length) + " byte(s)");
            return;  // one report is enough; the rest would repeat it
        }
        expected.push_back(cp);
    }

    const auto decoded = DecodeUtf8(all);
    if (!decoded) {
        check.Expect(false, "DecodeUtf8 rejected the encoding of all scalar values");
        return;
    }
    if (*decoded != expected) {
        std::size_t i = 0;
        while (i < decoded->size() && i < expected.size() && (*decoded)[i] == expected[i]) {
            ++i;
        }
        check.Expect(false, "round trip of all scalar values differs at index " +
                                std::to_string(i) + ", want U+" +
                                Hex(static_cast<std::uint32_t>(expected[i]), 4));
    }
}

/// What a naive encoder produces for a surrogate (ED A0..BF 80..BF) must be
/// rejected for every one of the 2048 surrogates.
void TestAllSurrogatesRejected(Checker& check) {
    for (std::uint32_t value = 0xD800; value <= 0xDFFF; ++value) {
        const std::string bytes =
            Bytes({0xED, 0x80u | ((value >> 6) & 0x3Fu), 0x80u | (value & 0x3Fu)});
        if (DecodeUtf8(bytes)) {
            check.Expect(false,
                         "surrogate U+" + Hex(value, 4) + " (" + HexBytes(bytes) + ") accepted");
            return;
        }
    }
}

void TestUtf16Length(Checker& check) {
    struct Case {
        char32_t cp;
        std::size_t want;
    };
    const Case cases[] = {
        {0x0041, 1}, {0x044F, 1}, {0xFFFF, 1}, {0x10000, 2}, {0x1F485, 2}, {0x10FFFF, 2},
    };
    for (const Case& c : cases) {
        check.Expect(Utf16Length(c.cp) == c.want,
                     "Utf16Length(U+" + Hex(static_cast<std::uint32_t>(c.cp), 4) + ") = " +
                         std::to_string(Utf16Length(c.cp)) + ", want " + std::to_string(c.want));
    }
}

}  // namespace

int main() {
    Checker check;
    TestValidSequences(check);
    TestInvalidSequences(check);
    TestAllScalarValues(check);
    TestAllSurrogatesRejected(check);
    TestUtf16Length(check);
    return check.ExitCode();
}
