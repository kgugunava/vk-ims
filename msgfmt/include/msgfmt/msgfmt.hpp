#pragma once

#include <cstddef>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace msgfmt {

enum class Style { kBold, kItalic, kCode, kLink };

/// A styled span of Message::text.
struct Range {
    Style style = Style::kBold;
    std::size_t offset = 0;  // UTF-16 code units from the start of Message::text
    std::size_t length = 0;  // UTF-16 code units
    std::string url;         // empty unless style == Style::kLink

    friend bool operator==(const Range& a, const Range& b) {
        return a.style == b.style && a.offset == b.offset && a.length == b.length && a.url == b.url;
    }
    friend bool operator!=(const Range& a, const Range& b) { return !(a == b); }
};

/// Plain text with style ranges, the form messenger bot APIs expect.
struct Message {
    std::string text;           // UTF-8, markup removed
    std::vector<Range> ranges;  // sorted by offset, then by length descending

    friend bool operator==(const Message& a, const Message& b) {
        return a.text == b.text && a.ranges == b.ranges;
    }
    friend bool operator!=(const Message& a, const Message& b) { return !(a == b); }
};

/// Parses markup in `input` (UTF-8 bytes) into plain text and style ranges.
/// Malformed markup is kept as literal text; the only error is invalid UTF-8,
/// reported as std::nullopt.
[[nodiscard]] std::optional<Message> Parse(std::string_view input);

/// Prefixes every special character (* _ ` [ ] ( ) \) with a backslash, so
/// Parse() of the result yields `input` unchanged and no ranges.
/// Returns std::nullopt if `input` is not valid UTF-8.
[[nodiscard]] std::optional<std::string> Escape(std::string_view input);

/// The JSON name of `style`: "bold", "italic", "code" or "link".
[[nodiscard]] std::string_view StyleName(Style style);

}  // namespace msgfmt
