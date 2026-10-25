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

/// For every position, the nearest position at or after it where each kind of
/// closing marker could stand. Built once in linear time, so each search is a
/// lookup: scanning the region per opener would make inputs with many
/// unmatched openers ("**a **a **a ...") quadratic.
///
/// Whether position j can close depends only on j and its neighbours, and on
/// whether j is escaped. Escapes are resolved in one pass from the start of
/// the input. A region scan would pair backslashes starting at its first
/// character instead, but every search starts right after an unescaped marker,
/// never in the middle of a pair, so both give the same pairs.
class CloserIndex {
public:
    explicit CloserIndex(const std::u32string& cp) : at_(cp.size() + 1) {
        const std::size_t n = cp.size();

        // Left to right: which characters are made literal by a backslash
        // before them (rule 9).
        for (std::size_t j = 0; j < n; ++j) {
            if (cp[j] == U'\\' && j + 1 < n && IsSpecial(cp[j + 1])) {
                at_[j + 1].escaped = true;
                ++j;
            }
        }

        // Right to left: each entry takes the next entry's answers and
        // replaces those for which position j itself qualifies.
        at_[n] = Nearest{n, n, n, n, n, n, false};
        for (std::size_t j = n; j-- > 0;) {
            const char32_t c = cp[j];
            const bool escaped = at_[j].escaped;
            // Rule 5 for every marker: a closer is preceded by non-whitespace.
            const bool after_text = j > 0 && !IsWhitespace(cp[j - 1]);

            Nearest next = at_[j + 1];
            next.escaped = escaped;
            if (c == U'*' && j + 1 < n && cp[j + 1] == U'*' && !escaped && after_text) {
                next.bold = j;
            }
            // Rule 3: a closing `_` is followed by the end of the input or by
            // a character that is not a letter or digit.
            if (c == U'_' && !escaped && after_text &&
                (j + 1 == n || !IsLetterOrDigit(cp[j + 1]))) {
                next.italic = j;
            }
            // Rule 7: escaping does not work in code, so a backslash cannot
            // protect a backtick there: in `a\` the second backtick closes.
            if (c == U'`' && after_text) {
                next.code = j;
            }
            // Rule 8: link text ends at the first unescaped `]`; the url ends
            // at the first `)`, taken verbatim, and may not contain whitespace.
            if (c == U']' && !escaped) {
                next.bracket = j;
            }
            if (c == U')') {
                next.paren = j;
            }
            if (IsWhitespace(c)) {
                next.whitespace = j;
            }
            at_[j] = next;
        }
    }

    // Each returns the nearest qualifying position >= from, or the input size
    // if there is none. `from` may be at most the input size.
    [[nodiscard]] std::size_t Bold(std::size_t from) const { return at_[from].bold; }
    [[nodiscard]] std::size_t Italic(std::size_t from) const { return at_[from].italic; }
    [[nodiscard]] std::size_t Code(std::size_t from) const { return at_[from].code; }
    [[nodiscard]] std::size_t Bracket(std::size_t from) const { return at_[from].bracket; }
    [[nodiscard]] std::size_t Paren(std::size_t from) const { return at_[from].paren; }
    [[nodiscard]] std::size_t Whitespace(std::size_t from) const { return at_[from].whitespace; }

private:
    // One entry per position, so the whole index is a single allocation:
    // Parse() runs millions of times on short inputs in the property test.
    struct Nearest {
        std::size_t bold = 0;
        std::size_t italic = 0;
        std::size_t code = 0;
        std::size_t bracket = 0;
        std::size_t paren = 0;
        std::size_t whitespace = 0;
        bool escaped = false;  // this position itself, not a nearest one
    };

    std::vector<Nearest> at_;
};

/// A range plus the position of its opening marker among all opened
/// ranges, which breaks ties between identical intervals (rule 12).
struct PendingRange {
    Range range;
    std::size_t open_order = 0;
};

class Parser {
public:
    explicit Parser(const std::u32string& input) : cp_(input), closers_(input) {}

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

    // The Find*Close functions return the nearest closer that lies inside the
    // region. The search starts one past the content start, so the content is
    // never empty. What counts as a closer is defined in CloserIndex.

    [[nodiscard]] std::optional<std::size_t> FindBoldClose(std::size_t content_begin,
                                                           std::size_t end) const {
        const std::size_t j = closers_.Bold(content_begin + 1);
        return j + 1 < end ? std::optional<std::size_t>(j) : std::nullopt;
    }

    /// Rule 3: `_` opens only at a word boundary, so snake_case stays text.
    /// The preceding character is taken from the whole input, not the region:
    /// a region always starts right after a marker, which is never a letter or
    /// digit, so the choice cannot change the result.
    [[nodiscard]] bool CanOpenItalic(std::size_t pos, std::size_t end) const {
        return (pos == 0 || !IsLetterOrDigit(cp_[pos - 1])) && CanOpen(pos + 1, end);
    }

    [[nodiscard]] std::optional<std::size_t> FindItalicClose(std::size_t content_begin,
                                                             std::size_t end) const {
        const std::size_t j = closers_.Italic(content_begin + 1);
        return j < end ? std::optional<std::size_t>(j) : std::nullopt;
    }

    [[nodiscard]] std::optional<std::size_t> FindCodeClose(std::size_t content_begin,
                                                           std::size_t end) const {
        const std::size_t j = closers_.Code(content_begin + 1);
        return j < end ? std::optional<std::size_t>(j) : std::nullopt;
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

        const std::size_t text_end = closers_.Bracket(open + 1);
        if (text_end >= end || text_end == open + 1 || IsWhitespace(cp_[text_end - 1])) {
            return std::nullopt;
        }

        const std::size_t url_begin = text_end + 2;
        if (url_begin > end || cp_[text_end + 1] != U'(') {
            return std::nullopt;
        }
        const std::size_t url_end = closers_.Paren(url_begin);
        if (url_end >= end || url_end == url_begin || closers_.Whitespace(url_begin) < url_end) {
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
    const CloserIndex closers_;
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
