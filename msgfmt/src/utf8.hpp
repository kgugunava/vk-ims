#pragma once

#include <cstddef>
#include <optional>
#include <string>
#include <string_view>

namespace msgfmt::detail {

/// Decodes UTF-8 into code points. Strict per RFC 3629 / Unicode Table 3-7:
/// overlong forms, surrogates (U+D800..U+DFFF), values above U+10FFFF, stray
/// continuation bytes and truncated sequences give std::nullopt.
/// U+0000 and U+FEFF are ordinary characters.
[[nodiscard]] std::optional<std::u32string> DecodeUtf8(std::string_view bytes);

/// Appends the UTF-8 encoding of `cp`, which must be a Unicode scalar value
/// (not a surrogate, at most U+10FFFF).
void AppendUtf8(std::string& out, char32_t cp);

/// Number of UTF-16 code units needed for `cp`: 1 inside the Basic
/// Multilingual Plane, 2 (a surrogate pair) above U+FFFF.
constexpr std::size_t Utf16Length(char32_t cp) noexcept { return cp > 0xFFFF ? 2 : 1; }

}  // namespace msgfmt::detail
