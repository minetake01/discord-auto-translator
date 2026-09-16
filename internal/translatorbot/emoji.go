package translatorbot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	emojiZWJ           = '\u200D'
	emojiVS15          = '\uFE0E'
	emojiVS16          = '\uFE0F'
	emojiKeycapCombine = '\u20E3'
	emojiTagSpace      = '\U000E0020'
	emojiTagCancel     = '\U000E007F'
	emojiSkinToneMin   = 0x1F3FB
	emojiSkinToneMax   = 0x1F3FF
)

func stripUnicodeEmojiSequences(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if n := unicodeEmojiSequenceLen(s[i:]); n > 0 {
			i += n
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += n
	}
	return b.String()
}

func unicodeEmojiSequenceLen(s string) int {
	if s == "" {
		return 0
	}
	r, n := utf8.DecodeRuneInString(s)
	if isKeycapBase(r) {
		rest := s[n:]
		total := n
		if r2, n2 := utf8.DecodeRuneInString(rest); r2 == emojiVS16 {
			rest = rest[n2:]
			total += n2
		}
		if r2, n2 := utf8.DecodeRuneInString(rest); r2 == emojiKeycapCombine {
			return total + n2
		}
		return 0
	}
	if unicode.Is(unicode.Regional_Indicator, r) {
		total := n
		rest := s[n:]
		for {
			r2, n2 := utf8.DecodeRuneInString(rest)
			if !unicode.Is(unicode.Regional_Indicator, r2) {
				break
			}
			rest = rest[n2:]
			total += n2
		}
		return total
	}
	if !unicode.In(r, extendedPictographic) {
		return 0
	}
	total := n
	rest := s[n:]
	rest, total = consumeMatchingRune(rest, total, isEmojiVariationSelector)
	rest, total = consumeMatchingRune(rest, total, isEmojiModifier)
	if nTags := emojiTagSequenceLen(rest); nTags > 0 {
		return total + nTags
	}
	for {
		r2, n2 := utf8.DecodeRuneInString(rest)
		if r2 != emojiZWJ {
			break
		}
		next := rest[n2:]
		r3, n3 := utf8.DecodeRuneInString(next)
		if !isEmojiBase(r3) {
			break
		}
		rest = next[n3:]
		total += n2 + n3
		rest, total = consumeMatchingRune(rest, total, isEmojiVariationSelector)
		rest, total = consumeMatchingRune(rest, total, isEmojiModifier)
	}
	return total
}

func isKeycapBase(r rune) bool {
	return r == '#' || r == '*' || (r >= '0' && r <= '9')
}

func isEmojiBase(r rune) bool {
	return unicode.Is(unicode.Regional_Indicator, r) || unicode.In(r, extendedPictographic)
}

func isEmojiVariationSelector(r rune) bool {
	return r == emojiVS15 || r == emojiVS16
}

func isEmojiModifier(r rune) bool {
	return r >= emojiSkinToneMin && r <= emojiSkinToneMax
}

func consumeMatchingRune(s string, total int, match func(rune) bool) (string, int) {
	r, n := utf8.DecodeRuneInString(s)
	if n > 0 && match(r) {
		return s[n:], total + n
	}
	return s, total
}

func emojiTagSequenceLen(s string) int {
	total := 0
	rest := s
	sawCancel := false
	for rest != "" {
		r, n := utf8.DecodeRuneInString(rest)
		if r < emojiTagSpace || r > emojiTagCancel {
			break
		}
		total += n
		rest = rest[n:]
		if r == emojiTagCancel {
			sawCancel = true
			break
		}
	}
	if !sawCancel {
		return 0
	}
	return total
}

// extendedPictographic is Unicode Extended_Pictographic from emoji-data.txt.
// ASCII digits and #/* are omitted so "1" stays translatable while "1️⃣" is not.
var extendedPictographic = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x00A9, 0x00A9, 1},
		{0x00AE, 0x00AE, 1},
		{0x203C, 0x203C, 1},
		{0x2049, 0x2049, 1},
		{0x2122, 0x2122, 1},
		{0x2139, 0x2139, 1},
		{0x2194, 0x2199, 1},
		{0x21A9, 0x21AA, 1},
		{0x231A, 0x231B, 1},
		{0x2328, 0x2328, 1},
		{0x23CF, 0x23CF, 1},
		{0x23E9, 0x23F3, 1},
		{0x23F8, 0x23FA, 1},
		{0x24C2, 0x24C2, 1},
		{0x25AA, 0x25AB, 1},
		{0x25B6, 0x25B6, 1},
		{0x25C0, 0x25C0, 1},
		{0x25FB, 0x25FE, 1},
		{0x2600, 0x2604, 1},
		{0x260E, 0x260E, 1},
		{0x2611, 0x2611, 1},
		{0x2614, 0x2615, 1},
		{0x2618, 0x2618, 1},
		{0x261D, 0x261D, 1},
		{0x2620, 0x2620, 1},
		{0x2622, 0x2623, 1},
		{0x2626, 0x2626, 1},
		{0x262A, 0x262A, 1},
		{0x262E, 0x262F, 1},
		{0x2638, 0x263A, 1},
		{0x2640, 0x2640, 1},
		{0x2642, 0x2642, 1},
		{0x2648, 0x2653, 1},
		{0x265F, 0x2660, 1},
		{0x2663, 0x2663, 1},
		{0x2665, 0x2666, 1},
		{0x2668, 0x2668, 1},
		{0x267B, 0x267B, 1},
		{0x267E, 0x267F, 1},
		{0x2692, 0x2697, 1},
		{0x2699, 0x2699, 1},
		{0x269B, 0x269C, 1},
		{0x26A0, 0x26A1, 1},
		{0x26A7, 0x26A7, 1},
		{0x26AA, 0x26AB, 1},
		{0x26B0, 0x26B1, 1},
		{0x26BD, 0x26BE, 1},
		{0x26C4, 0x26C5, 1},
		{0x26C8, 0x26C8, 1},
		{0x26CE, 0x26CF, 1},
		{0x26D1, 0x26D1, 1},
		{0x26D3, 0x26D4, 1},
		{0x26E9, 0x26EA, 1},
		{0x26F0, 0x26F5, 1},
		{0x26F7, 0x26FA, 1},
		{0x26FD, 0x26FD, 1},
		{0x2702, 0x2702, 1},
		{0x2705, 0x2705, 1},
		{0x2708, 0x270D, 1},
		{0x270F, 0x270F, 1},
		{0x2712, 0x2712, 1},
		{0x2714, 0x2714, 1},
		{0x2716, 0x2716, 1},
		{0x271D, 0x271D, 1},
		{0x2721, 0x2721, 1},
		{0x2728, 0x2728, 1},
		{0x2733, 0x2734, 1},
		{0x2744, 0x2744, 1},
		{0x2747, 0x2747, 1},
		{0x274C, 0x274C, 1},
		{0x274E, 0x274E, 1},
		{0x2753, 0x2755, 1},
		{0x2757, 0x2757, 1},
		{0x2763, 0x2764, 1},
		{0x2795, 0x2797, 1},
		{0x27A1, 0x27A1, 1},
		{0x27B0, 0x27B0, 1},
		{0x27BF, 0x27BF, 1},
		{0x2934, 0x2935, 1},
		{0x2B05, 0x2B07, 1},
		{0x2B1B, 0x2B1C, 1},
		{0x2B50, 0x2B50, 1},
		{0x2B55, 0x2B55, 1},
		{0x3030, 0x3030, 1},
		{0x303D, 0x303D, 1},
		{0x3297, 0x3297, 1},
		{0x3299, 0x3299, 1},
	},
	R32: []unicode.Range32{
		{0x1F004, 0x1F004, 1},
		{0x1F02C, 0x1F02F, 1},
		{0x1F094, 0x1F09F, 1},
		{0x1F0AF, 0x1F0B0, 1},
		{0x1F0C0, 0x1F0C0, 1},
		{0x1F0CF, 0x1F0D0, 1},
		{0x1F0F6, 0x1F0FF, 1},
		{0x1F170, 0x1F171, 1},
		{0x1F17E, 0x1F17F, 1},
		{0x1F18E, 0x1F18E, 1},
		{0x1F191, 0x1F19A, 1},
		{0x1F1AE, 0x1F1E5, 1},
		{0x1F201, 0x1F20F, 1},
		{0x1F21A, 0x1F21A, 1},
		{0x1F22F, 0x1F22F, 1},
		{0x1F232, 0x1F23A, 1},
		{0x1F23C, 0x1F23F, 1},
		{0x1F249, 0x1F25F, 1},
		{0x1F266, 0x1F321, 1},
		{0x1F324, 0x1F393, 1},
		{0x1F396, 0x1F397, 1},
		{0x1F399, 0x1F39B, 1},
		{0x1F39E, 0x1F3F0, 1},
		{0x1F3F3, 0x1F3F5, 1},
		{0x1F3F7, 0x1F3FA, 1},
		{0x1F400, 0x1F4FD, 1},
		{0x1F4FF, 0x1F53D, 1},
		{0x1F549, 0x1F54E, 1},
		{0x1F550, 0x1F567, 1},
		{0x1F56F, 0x1F570, 1},
		{0x1F573, 0x1F57A, 1},
		{0x1F587, 0x1F587, 1},
		{0x1F58A, 0x1F58D, 1},
		{0x1F590, 0x1F590, 1},
		{0x1F595, 0x1F596, 1},
		{0x1F5A4, 0x1F5A5, 1},
		{0x1F5A8, 0x1F5A8, 1},
		{0x1F5B1, 0x1F5B2, 1},
		{0x1F5BC, 0x1F5BC, 1},
		{0x1F5C2, 0x1F5C4, 1},
		{0x1F5D1, 0x1F5D3, 1},
		{0x1F5DC, 0x1F5DE, 1},
		{0x1F5E1, 0x1F5E1, 1},
		{0x1F5E3, 0x1F5E3, 1},
		{0x1F5E8, 0x1F5E8, 1},
		{0x1F5EF, 0x1F5EF, 1},
		{0x1F5F3, 0x1F5F3, 1},
		{0x1F5FA, 0x1F64F, 1},
		{0x1F680, 0x1F6C5, 1},
		{0x1F6CB, 0x1F6D2, 1},
		{0x1F6D5, 0x1F6E5, 1},
		{0x1F6E9, 0x1F6E9, 1},
		{0x1F6EB, 0x1F6F0, 1},
		{0x1F6F3, 0x1F6FF, 1},
		{0x1F7DA, 0x1F7FF, 1},
		{0x1F80C, 0x1F80F, 1},
		{0x1F848, 0x1F84F, 1},
		{0x1F85A, 0x1F85F, 1},
		{0x1F888, 0x1F88F, 1},
		{0x1F8AE, 0x1F8AF, 1},
		{0x1F8BC, 0x1F8BF, 1},
		{0x1F8C2, 0x1F8CF, 1},
		{0x1F8D9, 0x1F8FF, 1},
		{0x1F90C, 0x1F93A, 1},
		{0x1F93C, 0x1F945, 1},
		{0x1F947, 0x1F9FF, 1},
		{0x1FA58, 0x1FA5F, 1},
		{0x1FA6E, 0x1FAFF, 1},
		{0x1FC00, 0x1FFFD, 1},
	},
	LatinOffset: 2,
}
