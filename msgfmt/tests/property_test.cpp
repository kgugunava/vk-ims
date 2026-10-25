// Properties that must hold for every input, checked on generated inputs:
// every string of up to 6 characters over the special characters plus "a" and
// space, 100,000 random strings mixing markup with Cyrillic, emoji and
// combining marks, and random bytes (mostly invalid UTF-8).
//
// The random part is reproducible: the seed is fixed and printed on failure,
// and can be changed with the MSGFMT_SEED environment variable.

#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <optional>
#include <random>
#include <string>
#include <vector>

#include "msgfmt/msgfmt.hpp"
#include "test_util.hpp"
#include "unicode.hpp"
#include "utf8.hpp"

namespace {

using msgfmt::Message;
using msgfmt::Range;
using msgfmt::Style;
using msgfmt::detail::DecodeUtf8;
using msgfmt::detail::IsWhitespace;
using msgfmt::detail::Utf16Length;
using msgfmt::test::Checker;
using msgfmt::test::Describe;
using msgfmt::test::HexBytes;
using msgfmt::test::Quote;

constexpr std::uint32_t kDefaultSeed = 20261025;
constexpr int kRandomStrings = 100000;
constexpr std::size_t kMaxRandomTokens = 64;
constexpr int kRandomByteStrings = 20000;
constexpr std::size_t kMaxRandomBytes = 16;
constexpr std::size_t kExhaustiveMaxLength = 6;

// Stop reporting after this many failures: one bug tends to break thousands
// of generated inputs in the same way.
constexpr int kMaxReports = 10;

bool Contains(const Range& outer, const Range& inner) {
    return outer.offset <= inner.offset &&
           inner.offset + inner.length <= outer.offset + outer.length;
}

bool Disjoint(const Range& a, const Range& b) {
    return a.offset + a.length <= b.offset || b.offset + b.length <= a.offset;
}

/// Checks every invariant for one input; returns a description of the first
/// violated one, or an empty string.
std::string CheckInvariants(const std::string& input) {
    const std::optional<Message> parsed = msgfmt::Parse(input);
    if (!parsed) {
        return "valid UTF-8 rejected";
    }
    const Message& message = *parsed;

    // Determinism: the output depends only on the input.
    if (msgfmt::Parse(input) != parsed) {
        return "a second Parse() gave a different result";
    }

    // Markup is only removed, never added.
    if (message.text.size() > input.size()) {
        return "text is longer than the input";
    }

    const std::optional<std::u32string> text = DecodeUtf8(message.text);
    if (!text) {
        return "text is not valid UTF-8";
    }
    std::size_t text_units = 0;
    for (const char32_t cp : *text) {
        text_units += Utf16Length(cp);
    }

    const std::vector<Range>& ranges = message.ranges;
    for (std::size_t i = 0; i < ranges.size(); ++i) {
        const Range& range = ranges[i];
        if (range.length == 0) {
            return "empty range " + Describe(range);
        }
        if (range.offset + range.length > text_units) {
            return "range " + Describe(range) + " ends past the text (" +
                   std::to_string(text_units) + " units)";
        }
        if ((range.style == Style::kLink) == range.url.empty()) {
            return "url must be non-empty exactly for links: " + Describe(range);
        }
        const std::optional<std::u32string> url = DecodeUtf8(range.url);
        for (const char32_t cp : url.value_or(U"")) {
            if (IsWhitespace(cp) || cp == U')') {
                return "url contains whitespace or ')': " + Describe(range);
            }
        }

        if (i > 0) {
            // Rule 12: by offset, then longer first.
            const Range& prev = ranges[i - 1];
            if (prev.offset > range.offset ||
                (prev.offset == range.offset && prev.length < range.length)) {
                return "ranges out of order: " + Describe(prev) + " before " + Describe(range);
            }
        }

        for (std::size_t j = i + 1; j < ranges.size(); ++j) {
            const Range& later = ranges[j];
            if (Disjoint(range, later)) {
                continue;
            }
            // Sorting puts an enclosing range first, so any overlap must be
            // `later` lying inside `range`.
            if (!Contains(range, later)) {
                return "partial overlap: " + Describe(range) + " and " + Describe(later);
            }
            if (range.style == later.style) {
                return "same style nested (rule 10): " + Describe(range) + " and " +
                       Describe(later);
            }
            if (range.style == Style::kCode) {
                return "range inside code (rule 7): " + Describe(later);
            }
        }
    }

    // Escaping makes any text literal.
    const std::optional<std::string> escaped = msgfmt::Escape(input);
    if (!escaped) {
        return "Escape() rejected valid UTF-8";
    }
    const std::optional<Message> round_trip = msgfmt::Parse(*escaped);
    if (round_trip != Message{input, {}}) {
        return "Parse(Escape(input)) = " + Describe(round_trip) + ", want the input as text";
    }
    return "";
}

class Reporter {
public:
    explicit Reporter(Checker& check) : check_(check) {}

    /// Returns false once enough failures have been reported.
    bool Check(const std::string& input, const std::string& context) {
        const std::string problem = CheckInvariants(input);
        if (!problem.empty()) {
            check_.Expect(false, context + "\n  input: " + Quote(input) + "\n  " + problem);
            ++reports_;
        }
        return reports_ < kMaxReports;
    }

private:
    Checker& check_;
    int reports_ = 0;
};

void TestAllShortStrings(Checker& check) {
    const std::string alphabet = "*_`[]()\\a ";
    Reporter reporter(check);
    std::size_t count = 0;
    for (std::size_t length = 0; length <= kExhaustiveMaxLength; ++length) {
        std::vector<std::size_t> digits(length, 0);
        std::string input(length, alphabet[0]);
        while (true) {
            for (std::size_t k = 0; k < length; ++k) {
                input[k] = alphabet[digits[k]];
            }
            ++count;
            if (!reporter.Check(input, "exhaustive")) {
                return;
            }
            // Next string: increment the base-N number held in `digits`.
            std::size_t k = length;
            while (k > 0 && ++digits[k - 1] == alphabet.size()) {
                digits[k - 1] = 0;
                --k;
            }
            if (k == 0) {
                break;
            }
        }
    }
    std::cout << "exhaustive: " << count << " strings\n";
}

std::uint32_t SeedFromEnvironment() {
    if (const char* value = std::getenv("MSGFMT_SEED")) {
        return static_cast<std::uint32_t>(std::strtoul(value, nullptr, 10));
    }
    return kDefaultSeed;
}

/// Index in [0, n). Uses the raw engine output, which the standard fixes for
/// mt19937, rather than a distribution, whose results differ between
/// standard libraries; the small modulo bias does not matter here.
std::size_t Pick(std::mt19937& rng, std::size_t n) { return static_cast<std::size_t>(rng()) % n; }

void TestRandomStrings(Checker& check, std::uint32_t seed) {
    // Tokens rather than single characters, so that multi-unit and
    // multi-code-point characters appear next to every marker.
    const std::string tokens[] = {
        "*",    "**", "_", "`", "[", "]", "(", ")", "\\", "a", "b", "я", "1", " ", "\n",
        "😀",    // two UTF-16 units
        "й",     // decomposed short i: a letter followed by a combining mark
        " ",     // no-break space
        "](u)",  // makes complete links more likely
    };
    std::mt19937 rng(seed);
    Reporter reporter(check);
    for (int iteration = 0; iteration < kRandomStrings; ++iteration) {
        std::string input;
        const std::size_t count = Pick(rng, kMaxRandomTokens + 1);
        for (std::size_t k = 0; k < count; ++k) {
            input += tokens[Pick(rng, std::size(tokens))];
        }
        const std::string context =
            "random: seed " + std::to_string(seed) + ", iteration " + std::to_string(iteration);
        if (!reporter.Check(input, context)) {
            return;
        }
    }
}

void TestRandomBytes(Checker& check, std::uint32_t seed) {
    std::mt19937 rng(seed);
    int reports = 0;
    for (int iteration = 0; iteration < kRandomByteStrings && reports < kMaxReports; ++iteration) {
        std::string input(Pick(rng, kMaxRandomBytes + 1), '\0');
        for (char& c : input) {
            c = static_cast<char>(Pick(rng, 256));
        }
        // Both functions must agree with the decoder on validity, and must
        // not crash or trip a sanitizer on any bytes.
        const bool valid = DecodeUtf8(input).has_value();
        const bool parsed = msgfmt::Parse(input).has_value();
        const bool escaped = msgfmt::Escape(input).has_value();
        if (parsed != valid || escaped != valid) {
            check.Expect(false, "random bytes " + HexBytes(input) + ": valid=" +
                                    std::to_string(valid) + " parsed=" + std::to_string(parsed) +
                                    " escaped=" + std::to_string(escaped));
            ++reports;
        }
    }
}

}  // namespace

int main() {
    Checker check;
    const std::uint32_t seed = SeedFromEnvironment();
    TestAllShortStrings(check);
    TestRandomStrings(check, seed);
    TestRandomBytes(check, seed);
    return check.ExitCode();
}
