#include "json.hpp"

#include <cstddef>
#include <string_view>

namespace msgfmt::detail {

namespace {

/// Appends `utf8` as a JSON string literal. The input is valid UTF-8, so
/// multi-byte characters can be copied byte by byte: none of their bytes is
/// below 0x80, and only ASCII needs escaping.
void AppendJsonString(std::string& out, std::string_view utf8) {
    constexpr std::string_view kHex = "0123456789abcdef";
    out += '"';
    for (const char c : utf8) {
        switch (c) {
            case '"':
                out += "\\\"";
                break;
            case '\\':
                out += "\\\\";
                break;
            case '\b':
                out += "\\b";
                break;
            case '\f':
                out += "\\f";
                break;
            case '\n':
                out += "\\n";
                break;
            case '\r':
                out += "\\r";
                break;
            case '\t':
                out += "\\t";
                break;
            default: {
                const auto byte = static_cast<unsigned char>(c);
                if (byte < 0x20) {
                    out += "\\u00";
                    out += kHex[static_cast<std::size_t>(byte) >> 4];
                    out += kHex[static_cast<std::size_t>(byte) & 0xFu];
                } else {
                    out += c;
                }
            }
        }
    }
    out += '"';
}

}  // namespace

std::string ToJson(const Message& message) {
    std::string out = "{\"text\":";
    AppendJsonString(out, message.text);
    out += ",\"ranges\":[";
    for (std::size_t i = 0; i < message.ranges.size(); ++i) {
        const Range& range = message.ranges[i];
        if (i > 0) {
            out += ',';
        }
        out += "{\"style\":";
        AppendJsonString(out, StyleName(range.style));
        out += ",\"offset\":" + std::to_string(range.offset);
        out += ",\"length\":" + std::to_string(range.length);
        out += ",\"url\":";
        AppendJsonString(out, range.url);
        out += '}';
    }
    out += "]}";
    return out;
}

std::string InvalidUtf8Json() { return R"({"error":"invalid_utf8"})"; }

}  // namespace msgfmt::detail
