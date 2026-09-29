package chat

import (
	"strings"
	"unicode"

	"github.com/spf13/viper"
)

// removePunctuation strips punctuation from text
func removePunctuation(text string) string {
	// create a strings.Builder
	var builder strings.Builder
	builder.Grow(len(text))

	for _, r := range text {
		if !unicode.IsPunct(r) && !unicode.IsSpace(r) {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// isWakeupWord checks whether text is a wake word
func isWakeupWord(text string) bool {
	wakeupWords := viper.GetStringSlice("wakeup_words")
	for _, word := range wakeupWords {
		if text == word {
			return true
		}
	}
	return false
}
