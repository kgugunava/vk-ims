#include "msgfmt/msgfmt.hpp"

namespace msgfmt {

std::string_view StyleName(Style style) {
    switch (style) {
        case Style::kBold:
            return "bold";
        case Style::kItalic:
            return "italic";
        case Style::kCode:
            return "code";
        case Style::kLink:
            return "link";
    }
    // Only reachable with a value cast from outside the enum's range.
    return {};
}

}  // namespace msgfmt
