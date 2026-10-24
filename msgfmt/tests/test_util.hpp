#pragma once

#include <cstddef>
#include <iostream>
#include <optional>
#include <string>
#include <string_view>

#include "msgfmt/msgfmt.hpp"

namespace msgfmt::test {

/// Counts failed checks. A test's main() returns ExitCode(), so CTest marks
/// the test as failed if any check failed; checks after a failure still run.
class Checker {
public:
    /// Records a failure when `ok` is false; `context` names the case and
    /// should show the input and the got/want values.
    void Expect(bool ok, std::string_view context) {
        if (!ok) {
            ++failures_;
            std::cerr << "FAIL " << context << '\n';
        }
    }

    [[nodiscard]] int ExitCode() const {
        if (failures_ > 0) {
            std::cerr << failures_ << " check(s) failed\n";
            return 1;
        }
        return 0;
    }

private:
    int failures_ = 0;
};

/// Bytes as a double-quoted string; control bytes are shown as escapes so
/// that inputs like "a\nb" stay on one line in failure messages.
inline std::string Quote(std::string_view bytes) {
    constexpr std::string_view kHex = "0123456789ABCDEF";
    std::string out = "\"";
    for (const char c : bytes) {
        const auto byte = static_cast<unsigned char>(c);
        if (c == '"' || c == '\\') {
            out += '\\';
            out += c;
        } else if (c == '\n') {
            out += "\\n";
        } else if (c == '\t') {
            out += "\\t";
        } else if (byte < 0x20 || byte == 0x7F) {
            out += "\\x";
            out += kHex[static_cast<std::size_t>(byte) >> 4];
            out += kHex[static_cast<std::size_t>(byte) & 0xFu];
        } else {
            out += c;
        }
    }
    out += '"';
    return out;
}

/// Range in the notation used by the plan and the task: style(offset, length[, url]).
inline std::string Describe(const Range& range) {
    std::string out(StyleName(range.style));
    out += '(' + std::to_string(range.offset) + ", " + std::to_string(range.length);
    // Shown for non-links too, so a stray url on another style is visible.
    if (range.style == Style::kLink || !range.url.empty()) {
        out += ", " + Quote(range.url);
    }
    out += ')';
    return out;
}

inline std::string Describe(const Message& message) {
    std::string out = "{text: " + Quote(message.text) + ", ranges: [";
    for (std::size_t i = 0; i < message.ranges.size(); ++i) {
        if (i > 0) {
            out += ", ";
        }
        out += Describe(message.ranges[i]);
    }
    out += "]}";
    return out;
}

inline std::string Describe(const std::optional<Message>& message) {
    return message ? Describe(*message) : "nullopt";
}

}  // namespace msgfmt::test
