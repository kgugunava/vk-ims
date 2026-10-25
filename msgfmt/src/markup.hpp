#pragma once

namespace msgfmt::detail {

/// Characters with a meaning in the markup. A backslash before one of them
/// makes it literal (rule 9); Escape() puts a backslash before each of them.
constexpr bool IsSpecial(char32_t cp) noexcept {
    switch (cp) {
        case U'*':
        case U'_':
        case U'`':
        case U'[':
        case U']':
        case U'(':
        case U')':
        case U'\\':
            return true;
        default:
            return false;
    }
}

}  // namespace msgfmt::detail
