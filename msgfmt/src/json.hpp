#pragma once

#include <string>

#include "msgfmt/msgfmt.hpp"

namespace msgfmt::detail {

/// {"text": ..., "ranges": [{"style", "offset", "length", "url"}, ...]} on one
/// line, without a trailing newline. Exactly these fields, in this order.
/// Strings escape `"`, `\` and control characters below U+0020; all other
/// UTF-8 is written as is.
[[nodiscard]] std::string ToJson(const Message& message);

/// {"error":"invalid_utf8"}, without a trailing newline.
[[nodiscard]] std::string InvalidUtf8Json();

}  // namespace msgfmt::detail
