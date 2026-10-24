#include "utf8.hpp"

#include <cassert>
#include <cstdint>

namespace msgfmt::detail {

namespace {

/// Shape of a multi-byte sequence, determined by its lead byte.
struct SequenceShape {
    std::size_t length = 0;       // total bytes, 0 if the lead byte is invalid
    std::uint32_t lead_bits = 0;  // payload bits of the lead byte
    // Allowed range of the second byte. Narrower than 80..BF for E0, ED, F0
    // and F4: that is where overlong forms, surrogates and values above
    // U+10FFFF would otherwise slip through (Unicode Table 3-7).
    std::uint32_t second_min = 0x80;
    std::uint32_t second_max = 0xBF;
};

SequenceShape ShapeOf(std::uint32_t lead) {
    SequenceShape shape;
    if (lead >= 0xC2 && lead <= 0xDF) {  // C0 and C1 could only start overlong forms
        shape.length = 2;
        shape.lead_bits = lead & 0x1Fu;
    } else if (lead >= 0xE0 && lead <= 0xEF) {
        shape.length = 3;
        shape.lead_bits = lead & 0x0Fu;
        if (lead == 0xE0) {
            shape.second_min = 0xA0;  // below: overlong
        } else if (lead == 0xED) {
            shape.second_max = 0x9F;  // above: surrogates
        }
    } else if (lead >= 0xF0 && lead <= 0xF4) {
        shape.length = 4;
        shape.lead_bits = lead & 0x07u;
        if (lead == 0xF0) {
            shape.second_min = 0x90;  // below: overlong
        } else if (lead == 0xF4) {
            shape.second_max = 0x8F;  // above: beyond U+10FFFF
        }
    }
    return shape;
}

}  // namespace

std::optional<std::u32string> DecodeUtf8(std::string_view bytes) {
    std::u32string out;
    out.reserve(bytes.size());

    std::size_t i = 0;
    while (i < bytes.size()) {
        const std::uint32_t lead = static_cast<unsigned char>(bytes[i]);
        if (lead < 0x80) {
            out.push_back(static_cast<char32_t>(lead));
            ++i;
            continue;
        }

        const SequenceShape shape = ShapeOf(lead);
        if (shape.length == 0 || bytes.size() - i < shape.length) {
            return std::nullopt;
        }

        std::uint32_t cp = shape.lead_bits;
        for (std::size_t k = 1; k < shape.length; ++k) {
            const std::uint32_t byte = static_cast<unsigned char>(bytes[i + k]);
            const std::uint32_t min = k == 1 ? shape.second_min : 0x80;
            const std::uint32_t max = k == 1 ? shape.second_max : 0xBF;
            if (byte < min || byte > max) {
                return std::nullopt;
            }
            cp = (cp << 6) | (byte & 0x3Fu);
        }
        out.push_back(static_cast<char32_t>(cp));
        i += shape.length;
    }
    return out;
}

void AppendUtf8(std::string& out, char32_t cp) {
    assert(cp <= 0x10FFFF && (cp < 0xD800 || cp > 0xDFFF));

    const auto put = [&out](std::uint32_t byte) { out.push_back(static_cast<char>(byte)); };
    const auto value = static_cast<std::uint32_t>(cp);
    if (value < 0x80) {
        put(value);
    } else if (value < 0x800) {
        put(0xC0u | (value >> 6));
        put(0x80u | (value & 0x3Fu));
    } else if (value < 0x10000) {
        put(0xE0u | (value >> 12));
        put(0x80u | ((value >> 6) & 0x3Fu));
        put(0x80u | (value & 0x3Fu));
    } else {
        put(0xF0u | (value >> 18));
        put(0x80u | ((value >> 12) & 0x3Fu));
        put(0x80u | ((value >> 6) & 0x3Fu));
        put(0x80u | (value & 0x3Fu));
    }
}

}  // namespace msgfmt::detail
