package providers

import "unicode/utf8"

// TextLength is how long text is for a channel's MaxChars: characters, or X's weighted length when the limits
// say so.
func TextLength(l Limits, text string) int {
	if !l.XWeighted {
		return utf8.RuneCountInString(text)
	}
	return xLength(text)
}

// xLength counts text the way X does (twitter-text v3): Latin and most punctuation 1, everything else (CJK,
// emoji...) 2, an emoji sequence (ZWJ families, skin tones, flags, keycaps) 2 in all, and every link 23 whatever
// its length.
func xLength(text string) int {
	n := 0
	for _, u := range linkRe.FindAllString(text, -1) {
		if isWebLink(u) {
			n += 23 - utf8.RuneCountInString(u) // the runes are counted below; a link is 23 in all
		}
	}
	joined, flag := false, false
	for _, r := range text {
		switch {
		case r == 0x200D: // zero-width joiner: the next rune belongs to the same emoji
			joined = true
			continue
		case joined, r == 0xFE0F, r >= 0x1F3FB && r <= 0x1F3FF, r == 0x20E3, r >= 0xE0020 && r <= 0xE007F:
			joined = false // part of the emoji before it
			continue
		case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators: a pair is one flag
			if flag {
				flag = false
				continue
			}
			flag = true
			n += 2
			continue
		}
		flag = false
		if r <= 0x10FF || (r >= 0x2000 && r <= 0x200D) || (r >= 0x2010 && r <= 0x201F) || (r >= 0x2032 && r <= 0x2037) {
			n++
		} else {
			n += 2
		}
	}
	return n
}
