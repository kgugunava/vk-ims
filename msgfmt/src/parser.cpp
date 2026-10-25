// Single left-to-right pass over code points (rule 1). At an opening marker
// the parser looks ahead for its closing marker inside the current region;
// if found, the content between them is parsed recursively as a new region
// with that style marked active, otherwise the marker is emitted as text.
// Bounding the closing-marker search by the region keeps ranges either
// nested or disjoint, never partially overlapping (rule 12).

#include <algorithm>
#include <cassert>
#include <cstddef>
#include <optional>
#include <string>
#include <tuple>
#include <utility>
#include <vector>

#include "markup.hpp"
#include "msgfmt/msgfmt.hpp"
#include "unicode.hpp"
#include "utf8.hpp"

namespace msgfmt {

namespace {

using detail::AppendUtf8;
using detail::IsLetterOrDigit;
using detail::IsSpecial;
using detail::IsWhitespace;
using detail::Utf16Length;

/// Styles open around the region being parsed. A marker of a style that is
/// already open is plain text (rule 10).
///
/// The other rules already guarantee this, so the check never changes the
/// result. For bold and italic, an inner marker could only open if its closer
/// lay inside the region, but that closer would then have been the nearest
/// one for the outer marker and ended the region earlier. For links, the text
/// region ends at the first unescaped `]`, leaving an inner `[` no `]` to
/// pair with. Code content is never parsed. The check states rule 10
/// explicitly instead of relying on these arguments; they were confirmed by
/// comparing output with and without it on every input of up to 9 characters
/// over "*_a \" and up to 8 characters over "[]()a \*" (21.6M inputs).
class StyleSet {
public:
    [[nodiscard]] bool Has(Style style) const { return (bits_ & Bit(style)) != 0; }
    [[nodiscard]] StyleSet With(Style style) const {
        StyleSet result = *this;
        result.bits_ |= Bit(style);
        return result;
    }

private:
    static unsigned Bit(Style style) { return 1u << static_cast<unsigned>(style); }

    unsigned bits_ = 0;
};

/// A range plus the position of its opening marker among all opened
/// ranges, which breaks ties between identical intervals (rule 12).
struct PendingRange {
    Range range;
    std::size_t open_order = 0;
};

class Parser {
public:
    explicit Parser(const std::u32string& input) : cp_(input) {}

    Message Run() {
        ParseRegion(0, cp_.size(), StyleSet{});

        // Rule 12: by offset, then longer first, then the one opened earlier.
        std::sort(ranges_.begin(), ranges_.end(), [](const PendingRange& a, const PendingRange& b) {
            return std::make_tuple(a.range.offset, b.range.length, a.open_order) <
                   std::make_tuple(b.range.offset, a.range.length, b.open_order);
        });

        Message message;
        message.text = std::move(text_);
        message.ranges.reserve(ranges_.size());
        for (PendingRange& pending : ranges_) {
            message.ranges.push_back(std::move(pending.range));
        }
        return message;
    }

private:
    /// Parses cp_[begin, end) and appends its text and ranges to the output.
    void ParseRegion(std::size_t begin, std::size_t end, StyleSet active) {
        std::size_t i = begin;
        while (i < end) {
            const char32_t c = cp_[i];

            if (IsEscape(i, end)) {
                Append(cp_[i + 1]);
                i += 2;
                continue;
            }

            if (c == U'*' && i + 1 < end && cp_[i + 1] == U'*') {
                if (!active.Has(Style::kBold) && CanOpen(i + 2, end)) {
                    if (const auto close = FindBoldClose(i + 2, end)) {
                        EmitStyled(Style::kBold, i + 2, *close, active);
                        i = *close + 2;
                        continue;
                    }
                }
                // Both asterisks go out together: retrying the second one
                // as an opener would only ever see a lone '*'.
                Append(U'*');
                Append(U'*');
                i += 2;
                continue;
            }

            if (c == U'_') {
                if (!active.Has(Style::kItalic) && CanOpenItalic(i, end)) {
                    if (const auto close = FindItalicClose(i + 1, end)) {
                        EmitStyled(Style::kItalic, i + 1, *close, active);
                        i = *close + 1;
                        continue;
                    }
                }
                Append(U'_');
                ++i;
                continue;
            }

            // No active-set check: code content is never parsed, so a
            // backtick is never seen while code is open.
            if (c == U'`') {
                if (CanOpen(i + 1, end)) {
                    if (const auto close = FindCodeClose(i + 1, end)) {
                        EmitCode(i + 1, *close);
                        i = *close + 1;
                        continue;
                    }
                }
                Append(U'`');
                ++i;
                continue;
            }

            if (c == U'[') {
                if (!active.Has(Style::kLink)) {
                    if (const auto link = FindLink(i, end)) {
                        EmitStyled(Style::kLink, i + 1, link->text_end, active, link->url);
                        i = link->next;
                        continue;
                    }
                }
                // Only the bracket becomes text; what follows is parsed as
                // usual, so in "[**a**]()" the bold still applies.
                Append(U'[');
                ++i;
                continue;
            }

            // A lone '*', and every other character, is plain text.
            Append(c);
            ++i;
        }
    }

    /// Rule 9: a backslash right before a special character escapes it.
    [[nodiscard]] bool IsEscape(std::size_t pos, std::size_t end) const {
        return cp_[pos] == U'\\' && pos + 1 < end && IsSpecial(cp_[pos + 1]);
    }

    /// Rule 4: an opening marker must be followed by a non-whitespace
    /// character inside the region.
    [[nodiscard]] bool CanOpen(std::size_t content_begin, std::size_t end) const {
        return content_begin < end && !IsWhitespace(cp_[content_begin]);
    }

    /// Rule 5: the nearest "**" preceded by a non-whitespace character.
    /// Escaped characters are skipped, so "\**" never closes. The search starts
    /// one past the content start, so the content is never empty.
    [[nodiscard]] std::optional<std::size_t> FindBoldClose(std::size_t content_begin,
                                                           std::size_t end) const {
        std::size_t j = content_begin;
        while (j + 1 < end) {
            if (IsEscape(j, end)) {
                j += 2;
                continue;
            }
            if (j > content_begin && cp_[j] == U'*' && cp_[j + 1] == U'*' &&
                !IsWhitespace(cp_[j - 1])) {
                return j;
            }
            ++j;
        }
        return std::nullopt;
    }

    /// Rule 3: `_` opens only at a word boundary, so snake_case stays text.
    /// The preceding character is taken from the whole input, not the region:
    /// a region always starts right after a marker, which is never a letter or
    /// digit, so the choice cannot change the result.
    [[nodiscard]] bool CanOpenItalic(std::size_t pos, std::size_t end) const {
        return (pos == 0 || !IsLetterOrDigit(cp_[pos - 1])) && CanOpen(pos + 1, end);
    }

    /// Rules 3 and 5: the nearest `_` preceded by non-whitespace and followed
    /// by the end of the input or a character that is not a letter or digit.
    [[nodiscard]] std::optional<std::size_t> FindItalicClose(std::size_t content_begin,
                                                             std::size_t end) const {
        std::size_t j = content_begin;
        while (j < end) {
            if (IsEscape(j, end)) {
                j += 2;
                continue;
            }
            if (j > content_begin && cp_[j] == U'_' && !IsWhitespace(cp_[j - 1]) &&
                (j + 1 == cp_.size() || !IsLetterOrDigit(cp_[j + 1]))) {
                return j;
            }
            ++j;
        }
        return std::nullopt;
    }

    /// Rule 5: the nearest backtick preceded by non-whitespace. Unlike the
    /// other searches this one ignores backslashes: escaping does not work in
    /// code (rule 7), so in `a\` the second backtick closes.
    [[nodiscard]] std::optional<std::size_t> FindCodeClose(std::size_t content_begin,
                                                           std::size_t end) const {
        for (std::size_t j = content_begin + 1; j < end; ++j) {
            if (cp_[j] == U'`' && !IsWhitespace(cp_[j - 1])) {
                return j;
            }
        }
        return std::nullopt;
    }

    /// Rule 7: code content goes to the output exactly as written, with no
    /// markup and no escapes, so it holds no nested ranges.
    void EmitCode(std::size_t content_begin, std::size_t content_end) {
        const std::size_t start = utf16_size_;
        const std::size_t order = next_open_order_++;
        for (std::size_t k = content_begin; k < content_end; ++k) {
            Append(cp_[k]);
        }
        assert(utf16_size_ > start);
        ranges_.push_back({Range{Style::kCode, start, utf16_size_ - start, std::string()}, order});
    }

    struct LinkParts {
        std::size_t text_end = 0;  // position of the closing ']'
        std::string url;           // UTF-8
        std::size_t next = 0;      // position after the closing ')'
    };

    /// Rule 8: "[text](url)" with `[` at `open`. The text is non-empty and has
    /// no unescaped `]` (an escaped one is ordinary text), so the first
    /// unescaped `]` ends it; rule 5 applies to that `]` too. The url follows
    /// immediately in parentheses: non-empty, no whitespace, no `)`. It is
    /// taken verbatim, without escape processing, so a backslash stays part of
    /// it. Everything must fit inside the region.
    [[nodiscard]] std::optional<LinkParts> FindLink(std::size_t open, std::size_t end) const {
        if (!CanOpen(open + 1, end)) {
            return std::nullopt;
        }

        std::size_t text_end = open + 1;
        while (text_end < end && cp_[text_end] != U']') {
            if (IsEscape(text_end, end)) {
                ++text_end;  // the escaped character, possibly a ']', is skipped too
            }
            ++text_end;
        }
        if (text_end >= end || text_end == open + 1 || IsWhitespace(cp_[text_end - 1])) {
            return std::nullopt;
        }

        const std::size_t url_begin = text_end + 2;
        if (url_begin > end || cp_[text_end + 1] != U'(') {
            return std::nullopt;
        }
        std::size_t url_end = url_begin;
        while (url_end < end && cp_[url_end] != U')') {
            if (IsWhitespace(cp_[url_end])) {
                return std::nullopt;
            }
            ++url_end;
        }
        if (url_end >= end || url_end == url_begin) {
            return std::nullopt;
        }

        LinkParts link;
        link.text_end = text_end;
        for (std::size_t k = url_begin; k < url_end; ++k) {
            AppendUtf8(link.url, cp_[k]);
        }
        link.next = url_end + 1;
        return link;
    }

    /// Parses cp_[content_begin, content_end) with `style` added to the
    /// active set and records a range over the text it produced. `url` is
    /// non-empty only for links.
    void EmitStyled(Style style, std::size_t content_begin, std::size_t content_end,
                    StyleSet active, std::string url = std::string()) {
        const std::size_t start = utf16_size_;
        // Taken before the content is parsed, so an outer range always
        // precedes the ranges nested in it.
        const std::size_t order = next_open_order_++;

        ParseRegion(content_begin, content_end, active.With(style));

        // Content is non-empty and every character in it produces output
        // (an escape pair produces the escaped character).
        assert(utf16_size_ > start);
        ranges_.push_back({Range{style, start, utf16_size_ - start, std::move(url)}, order});
    }

    void Append(char32_t c) {
        AppendUtf8(text_, c);
        utf16_size_ += Utf16Length(c);
    }

    const std::u32string& cp_;
    std::string text_;
    std::size_t utf16_size_ = 0;  // length of text_ in UTF-16 code units
    std::vector<PendingRange> ranges_;
    std::size_t next_open_order_ = 0;
};

}  // namespace

std::optional<Message> Parse(std::string_view input) {
    const std::optional<std::u32string> code_points = detail::DecodeUtf8(input);
    if (!code_points) {
        return std::nullopt;
    }
    return Parser(*code_points).Run();
}

}  // namespace msgfmt
