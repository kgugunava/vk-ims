#include <optional>
#include <string>
#include <string_view>

#include "markup.hpp"
#include "msgfmt/msgfmt.hpp"
#include "utf8.hpp"

namespace msgfmt {

std::optional<std::string> Escape(std::string_view input) {
    // Decoding only validates the input. The escaping itself works on bytes:
    // every special character is ASCII, and in UTF-8 an ASCII byte never
    // occurs inside a multi-byte sequence.
    if (!detail::DecodeUtf8(input)) {
        return std::nullopt;
    }

    std::string out;
    out.reserve(input.size());
    for (const char c : input) {
        if (detail::IsSpecial(static_cast<unsigned char>(c))) {
            out += '\\';
        }
        out += c;
    }
    return out;
}

}  // namespace msgfmt
