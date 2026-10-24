#pragma once

namespace msgfmt::detail {

/// Letter or digit for the word-boundary rule of `_`: Unicode general
/// categories L* and Nd. Combining marks (Mn, Mc) are not letters, so a
/// decomposed "й" (U+0438 U+0306) ends in a non-letter.
[[nodiscard]] bool IsLetterOrDigit(char32_t cp) noexcept;

/// Whitespace per the Unicode White_Space property (PropList.txt).
[[nodiscard]] bool IsWhitespace(char32_t cp) noexcept;

}  // namespace msgfmt::detail
