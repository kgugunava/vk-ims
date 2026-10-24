#include "unicode.hpp"

#include <algorithm>
#include <array>
#include <cstddef>
#include <iterator>

namespace msgfmt::detail {

namespace {

struct CodePointRange {
    char32_t first;
    char32_t last;  // inclusive
};

#include "unicode_tables.inc"

// White_Space from Unicode PropList.txt; it has not changed since Unicode 6.
// Python's str.isspace() is not a substitute: it also accepts U+001C..U+001F.
constexpr std::array<CodePointRange, 10> kWhitespaceRanges = {{
    {0x0009, 0x000D},  // \t \n \v \f \r
    {0x0020, 0x0020},  // space
    {0x0085, 0x0085},  // next line
    {0x00A0, 0x00A0},  // no-break space
    {0x1680, 0x1680},  // ogham space mark
    {0x2000, 0x200A},  // en quad .. hair space
    {0x2028, 0x2029},  // line and paragraph separators
    {0x202F, 0x202F},  // narrow no-break space
    {0x205F, 0x205F},  // medium mathematical space
    {0x3000, 0x3000},  // ideographic space
}};

/// Lookup by binary search relies on this; checked at compile time so a bad
/// regeneration of the table fails the build instead of misclassifying.
template <std::size_t N>
constexpr bool IsSortedAndDisjoint(const std::array<CodePointRange, N>& ranges) {
    for (std::size_t i = 0; i < N; ++i) {
        if (ranges[i].first > ranges[i].last) {
            return false;
        }
        if (i > 0 && ranges[i - 1].last >= ranges[i].first) {
            return false;
        }
    }
    return true;
}

static_assert(IsSortedAndDisjoint(kLetterOrDigitRanges));
static_assert(IsSortedAndDisjoint(kWhitespaceRanges));

template <std::size_t N>
bool Contains(const std::array<CodePointRange, N>& ranges, char32_t cp) {
    // The only candidate is the last range that starts at or before cp.
    const auto after = std::upper_bound(
        ranges.begin(), ranges.end(), cp,
        [](char32_t value, const CodePointRange& range) { return value < range.first; });
    return after != ranges.begin() && cp <= std::prev(after)->last;
}

}  // namespace

bool IsLetterOrDigit(char32_t cp) noexcept {
    if (cp < 0x80) {
        return (cp >= U'0' && cp <= U'9') || (cp >= U'A' && cp <= U'Z') ||
               (cp >= U'a' && cp <= U'z');
    }
    return Contains(kLetterOrDigitRanges, cp);
}

bool IsWhitespace(char32_t cp) noexcept { return Contains(kWhitespaceRanges, cp); }

}  // namespace msgfmt::detail
