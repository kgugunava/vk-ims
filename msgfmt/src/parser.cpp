#include "msgfmt/msgfmt.hpp"

namespace msgfmt {

std::optional<Message> Parse(std::string_view input) {
    // Skeleton: markup is not interpreted and UTF-8 is not validated yet.
    return Message{std::string(input), {}};
}

}  // namespace msgfmt
