package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var (
	// Punctuation set
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

	// Object pool for reuse
	builderPool = sync.Pool{
		New: func() interface{} {
			return &strings.Builder{}
		},
	}

	// Slice pool for results
	runeSlicePool = sync.Pool{
		New: func() interface{} {
			slice := make([]rune, 0, 1024)
			return &slice
		},
	}

	// Precompiled regex
	numberPrefixRegex = regexp.MustCompile(`(?m)^[\s]*\d{1,3}\.$`)
)

// Fast char check instead of regex
func isNumberPrefix(text []rune, pos int) bool {
	if pos <= 0 || text[pos] != '.' {
		return false
	}

	// Scan back to line start or newline
	start := pos - 1
	digitCount := 0
	foundDigit := false

	// Skip whitespace before the dot
	for start >= 0 && (text[start] == ' ' || text[start] == '\t') {
		start--
	}

	// Count digits
	for start >= 0 && text[start] >= '0' && text[start] <= '9' {
		digitCount++
		foundDigit = true
		if digitCount > 3 { // More than 3 digits is not a valid ordinal
			return false
		}
		start--
	}

	// Check digit is preceded by whitespace or line start
	if start >= 0 && text[start] != ' ' && text[start] != '\t' && text[start] != '\n' {
		return false
	}

	return foundDigit
}

// Trim leading/trailing whitespace
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

func findLastPunctuation(text []rune) int {
	// Find last punctuation from the end
	lastPos := -1
	for i := len(text) - 1; i >= 0; i-- {
		// Check if punctuation
		if punctuationMap[text[i]] {
			// If dot, check whether it is part of an ordinal
			if text[i] == '.' && isNumberPrefix(text, i) {
				continue
			}
			return i
		}
	}
	return lastPos
}

func findNextSplitPoint(text []rune, startPos int, maxLen int) int {
	// Compute search end position
	endPos := startPos + maxLen
	if endPos > len(text) {
		endPos = len(text)
	}

	// Search forward
	for i := startPos; i < endPos; i++ {
		// Check newline and whether next line starts an ordinal
		if text[i] == '\n' {
			nextPos := i + 1
			// Skip whitespace
			for nextPos < endPos && (text[nextPos] == ' ' || text[nextPos] == '\t') {
				nextPos++
			}
			// Check whether an ordinal starts here
			if nextPos < endPos-2 && text[nextPos] >= '0' && text[nextPos] <= '9' {
				return i
			}
			continue
		}

		// Use map to check punctuation
		if punctuationMap[text[i]] {
			return i
		}
	}

	// If not found within maxLen, search a wider range
	if endPos < len(text) {
		for i := endPos; i < len(text); i++ {
			if text[i] == '\n' || punctuationMap[text[i]] {
				return i
			}
		}
	}

	return -1
}

func extractSmartSentences(text string, minLen, maxLen int) (sentences []string, remaining string) {
	// Preallocate a reasonable slice capacity
	estimatedCount := len(text) / 50
	if estimatedCount < 10 {
		estimatedCount = 10
	}
	sentences = make([]string, 0, estimatedCount)

	// Convert to rune slice once
	currentRunes := []rune(text)
	startPos := 0

	// Get reusable object from pool
	builder := builderPool.Get().(*strings.Builder)
	defer builderPool.Put(builder)
	builder.Grow(maxLen * 2)

	// Get temporary rune slice
	tempRunesPtr := runeSlicePool.Get().(*[]rune)
	tempRunes := (*tempRunesPtr)[:0]
	defer runeSlicePool.Put(tempRunesPtr)

	for startPos < len(currentRunes) {
		// Skip leading whitespace
		for startPos < len(currentRunes) && (currentRunes[startPos] == ' ' || currentRunes[startPos] == '\t' || currentRunes[startPos] == '\n') {
			startPos++
		}

		if startPos >= len(currentRunes) {
			break
		}

		// Find next split point
		splitPos := findNextSplitPoint(currentRunes, startPos, maxLen)
		if splitPos == -1 {
			// No split point; treat rest as remaining
			segment := trimSpaceRunes(currentRunes[startPos:])
			if len(segment) > 0 {
				remaining = string(segment)
			}
			break
		}

		// Extract current segment
		builder.Reset()
		tempRunes = tempRunes[:0]

		// Collect and process current segment
		segment := trimSpaceRunes(currentRunes[startPos : splitPos+1])

		// Check segment meets min length and ends with punctuation
		if len(segment) >= minLen && punctuationMap[segment[len(segment)-1]] {
			sentences = append(sentences, string(segment))
		} else {
			// If not, append to remaining
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

func main() {
	text := `厚,人家就晓得你又在敷衍我!每次问你都没有,你是不是不喜欢我了啦?哼,人家要生气喽!不跟你好了!除非...你答应我,等下带人家去夜市吃豆花啦~还要牵人家手手逛大街,一路上都要逗人家笑,逗得人家开心到飞上天!不然人家真的会不理你哦~`
	sentences, remaining := extractSmartSentences(text, 3, 200)
	for i, sentence := range sentences {
		fmt.Printf("\n句子%d:\n%s\n", i+1, sentence)
	}
	if remaining != "" {
		fmt.Printf("\n剩余:\n%s\n", remaining)
	}
}
