// msgfmt reads UTF-8 bytes from stdin. Without arguments it prints the parsed
// message as JSON; with `escape` it prints the input with every special
// character escaped.

#include <iostream>
#include <iterator>
#include <optional>
#include <string>
#include <string_view>
#include <utility>

#include "json.hpp"
#include "msgfmt/msgfmt.hpp"

namespace {

constexpr int kExitOk = 0;
constexpr int kExitFailure = 1;      // bad arguments, or stdin/stdout failed
constexpr int kExitInvalidUtf8 = 2;  // the only error defined by the task

enum class Command { kParse, kEscape };

std::optional<Command> CommandFromArgs(int argc, char* argv[]) {
    if (argc == 1) {
        return Command::kParse;
    }
    if (argc == 2 && std::string_view(argv[1]) == "escape") {
        return Command::kEscape;
    }
    return std::nullopt;
}

}  // namespace

int main(int argc, char* argv[]) {
    const std::optional<Command> command = CommandFromArgs(argc, argv);
    if (!command) {
        std::cerr << "usage: msgfmt [escape] < input\n";
        return kExitFailure;
    }

    const std::istreambuf_iterator<char> begin(std::cin);
    const std::istreambuf_iterator<char> end;
    const std::string input(begin, end);
    if (std::cin.bad()) {
        std::cerr << "msgfmt: cannot read stdin\n";
        return kExitFailure;
    }

    std::string output;
    int exit_code = kExitOk;
    if (*command == Command::kParse) {
        if (const std::optional<msgfmt::Message> message = msgfmt::Parse(input)) {
            output = msgfmt::detail::ToJson(*message) + '\n';
        } else {
            output = msgfmt::detail::InvalidUtf8Json() + '\n';
            exit_code = kExitInvalidUtf8;
        }
    } else if (std::optional<std::string> escaped = msgfmt::Escape(input)) {
        // No newline is added, so the output can be fed back to msgfmt.
        output = std::move(*escaped);
    } else {
        output = msgfmt::detail::InvalidUtf8Json() + '\n';
        exit_code = kExitInvalidUtf8;
    }

    std::cout.write(output.data(), static_cast<std::streamsize>(output.size()));
    std::cout.flush();
    if (!std::cout) {
        std::cerr << "msgfmt: cannot write stdout\n";
        return kExitFailure;
    }
    return exit_code;
}
