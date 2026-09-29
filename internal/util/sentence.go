package util

import (
	"bytes"
	"strings"
	"sync"
	"unicode"
)

var (
	// punctuationMap map of sentence-end and pause punctuation
	punctuationMap = map[rune]bool{
		'。':  true,
		'？':  true,
		'！':  true,
		'；':  true,
		'：':  true,
		'\n': true,
		'.':  true,
		'?':  true,
		'!':  true,
		';':  true,
		':':  true,
	}

	// firstPunctuation punctuation map for first pass (includes comma)
	firstPunctuation = map[rune]bool{
		'，':  true,
		',':  true,
		'。':  true,
		'？':  true,
		'！':  true,
		'；':  true,
		'：':  true,
		'\n': true,
		'.':  true,
		'?':  true,
		'!':  true,
		';':  true,
		':':  true,
	}

	// sentence-end punctuation
	sentenceEndPunctuation = []rune{'.', '。', '!', '！', '?', '？', '\n'}

	// sentence-pause punctuation (split points for long sentences)
	sentencePausePunctuation = []rune{',', '，', ';', '；', ':', '：'}

	// object pool for reuse
	builderPool = sync.Pool{
		New: func() interface{} {
			return &strings.Builder{}
		},
	}

	// slice pool for results
	runeSlicePool = sync.Pool{
		New: func() interface{} {
			slice := make([]rune, 0, 1024)
			return &slice
		},
	}
)

// IsSentenceEndPunctuation reports whether a rune is sentence-end punctuation
func IsSentenceEndPunctuation(r rune) bool {
	for _, p := range sentenceEndPunctuation {
		if r == p {
			return true
		}
	}
	return false
}

// IsSentencePausePunctuation reports whether a rune is sentence-pause punctuation
func IsSentencePausePunctuation(r rune) bool {
	for _, p := range sentencePausePunctuation {
		if r == p {
			return true
		}
	}
	return false
}

// IsNumberWithDot reports whether a string looks like a numbered prefix (e.g. "1.", "2.")
func IsNumberWithDot(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 || trimmed[len(trimmed)-1] != '.' {
		return false
	}

	for i := 0; i < len(trimmed)-1; i++ {
		if !unicode.IsDigit(rune(trimmed[i])) {
			return false
		}
	}
	return true
}

// ExtractCompleteSentences extracts complete sentences from text
// returns complete sentences and any unfinished remainder
func ExtractCompleteSentences(text string) ([]string, string) {
	if text == "" {
		return []string{}, ""
	}

	var sentences []string
	var currentSentence bytes.Buffer

	runes := []rune(text)
	lastIndex := len(runes) - 1

	for i, r := range runes {
		currentSentence.WriteRune(r)

		// check whether the sentence has ended
		if IsSentenceEndPunctuation(r) {
			// if this is sentence-end punctuation
			sentence := strings.TrimSpace(currentSentence.String())
			if sentence != "" {
				sentences = append(sentences, sentence)
			}
			currentSentence.Reset()
		} else if i == lastIndex {
			// if this is the last rune and not end punctuation, keep it in remaining
			break
		}
	}

	// return the unfinished sentence as remaining
	remaining := currentSentence.String()
	return sentences, strings.TrimSpace(remaining)
}

// isNumberPrefix fast rune checks instead of regex to detect numbered prefixes
func isNumberPrefix(text []rune, pos int) bool {
	if pos <= 0 || text[pos] != '.' {
		return false
	}

	// scan backward for line start or newline
	start := pos - 1
	digitCount := 0
	foundDigit := false

	// skip whitespace before the dot
	for start >= 0 && (text[start] == ' ' || text[start] == '\t') {
		start--
	}

	// count digits
	for start >= 0 && text[start] >= '0' && text[start] <= '9' {
		digitCount++
		foundDigit = true
		if digitCount > 3 { // more than 3 digits is not a valid numbered prefix
			return false
		}
		start--
	}

	// check that the digits are preceded by whitespace or line start
	if start >= 0 && text[start] != ' ' && text[start] != '\t' && text[start] != '\n' {
		return false
	}

	return foundDigit
}

// trimSpaceRunes trims leading/trailing whitespace runes
func trimSpaceRunes(text []rune) []rune {
	start, end := 0, len(text)-1

	for start <= end && (text[start] == ' ' || text[start] == '\t' || text[start] == '\n') {
		start++
	}

	for end >= start && (text[end] == ' ' || text[end] == '\t' || text[end] == '\n') {
		end--
	}

	if start > end {
		return nil
	}
	return text[start : end+1]
}

func isDigitAdjacentColon(text []rune, pos int) bool {
	if pos < 0 || pos >= len(text) {
		return false
	}

	colon := text[pos]
	if colon != ':' && colon != '：' {
		return false
	}

	if pos == 0 || !unicode.IsDigit(text[pos-1]) {
		return false
	}

	if pos == len(text)-1 {
		return true
	}

	return unicode.IsDigit(text[pos+1])
}

// findLastPunctuation finds the last punctuation scanning backward
func findLastPunctuation(text []rune, separatorMap map[rune]bool) int {
	lastPos := -1
	for i := len(text) - 1; i >= 0; i-- {
		// check whether it is punctuation
		if separatorMap[text[i]] {
			// if it is a dot, check whether it is part of a numbered prefix
			if text[i] == '.' && isNumberPrefix(text, i) {
				continue
			}
			if isDigitAdjacentColon(text, i) {
				continue
			}
			return i
		}
	}
	return lastPos
}

// findNextSplitPoint finds the next split point
func findNextSplitPoint(text []rune, startPos int, maxLen int, separatorMap map[rune]bool) int {
	// compute the search end position
	endPos := startPos + maxLen
	if endPos > len(text) {
		endPos = len(text)
	}

	// scan forward
	for i := startPos; i < endPos; i++ {
		// check for newline and whether the next line starts a numbered item
		if text[i] == '\n' {
			nextPos := i + 1
			// skip whitespace
			for nextPos < endPos && (text[nextPos] == ' ' || text[nextPos] == '\t') {
				nextPos++
			}
			// check whether a numbered item starts here
			if nextPos < endPos-2 && text[nextPos] >= '0' && text[nextPos] <= '9' {
				return i
			}
			continue
		}

		// use the map to check for punctuation
		if separatorMap[text[i]] {
			if isDigitAdjacentColon(text, i) {
				continue
			}
			return i
		}
	}

	// if none found within maxLen, try a wider range
	if endPos < len(text) {
		for i := endPos; i < len(text); i++ {
			if text[i] == '\n' {
				return i
			}
			if separatorMap[text[i]] {
				if isDigitAdjacentColon(text, i) {
					continue
				}
				return i
			}
		}
	}

	return -1
}

// ExtractSmartSentences smart sentence extraction
// text: input text
// minLen: minimum sentence length
// maxLen: maximum sentence length
// isFirst: first pass (allows comma as a separator)
func ExtractSmartSentences(text string, minLen, maxLen int, isFirst bool) (sentences []string, remaining string) {
	// when isFirst is true, allow comma as a separator
	separatorMap := punctuationMap
	if isFirst {
		separatorMap = firstPunctuation
	}
	// preallocate a reasonable slice capacity
	estimatedCount := len(text) / 50
	if estimatedCount < 10 {
		estimatedCount = 10
	}
	sentences = make([]string, 0, estimatedCount)

	// convert to a rune slice once
	currentRunes := []rune(text)
	startPos := 0

	// get a reusable object from the pool
	builder := builderPool.Get().(*strings.Builder)
	defer builderPool.Put(builder)
	builder.Grow(maxLen * 2)

	// get a temporary rune slice
	tempRunesPtr := runeSlicePool.Get().(*[]rune)
	tempRunes := (*tempRunesPtr)[:0]
	defer runeSlicePool.Put(tempRunesPtr)

	for startPos < len(currentRunes) {
		// skip leading whitespace
		for startPos < len(currentRunes) && (currentRunes[startPos] == ' ' || currentRunes[startPos] == '\t' || currentRunes[startPos] == '\n') {
			startPos++
		}

		if startPos >= len(currentRunes) {
			break
		}

		// finds the next split point
		splitPos := findNextSplitPoint(currentRunes, startPos, maxLen, separatorMap)
		if splitPos == -1 {
			// no split point; treat the rest as remaining
			segment := trimSpaceRunes(currentRunes[startPos:])
			if len(segment) > 0 {
				remaining = string(segment)
			}
			break
		}

		// extract the current segment
		builder.Reset()
		tempRunes = tempRunes[:0]

		// collect and process the current segment
		segment := trimSpaceRunes(currentRunes[startPos : splitPos+1])

		// require min length and ending punctuation
		if len(segment) >= minLen && separatorMap[segment[len(segment)-1]] {
			sentences = append(sentences, string(segment))
		} else {
			// if conditions fail, append to remaining
			if len(segment) > 0 {
				if len(remaining) > 0 {
					remaining += " "
				}
				remaining += string(segment)
			}
		}

		startPos = splitPos + 1
	}

	return sentences, remaining
}

// ContainsSentenceSeparator reports whether the string contains a sentence separator (end or pause punctuation)
func ContainsSentenceSeparator(s string, isFirst bool) bool {
	separatorMap := punctuationMap
	if isFirst {
		separatorMap = firstPunctuation
	}

	runes := []rune(s)
	for i, r := range runes {
		if !separatorMap[r] {
			continue
		}
		if isDigitAdjacentColon(runes, i) {
			continue
		}
		return true
	}

	return false
}
