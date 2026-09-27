package service

import (
	"strings"
)

// LooksLikeCommitment is a cheap check run before paying for extraction.
//
// Two reasons it exists. Extraction costs a model call per turn, which on the
// Gemini free tier is a meaningful share of the day's budget. And it only looks
// at what YOU wrote: a day plan the assistant proposes is a suggestion, not a
// promise, and extracting its bullet points would fill the list with things
// you never said you would do.
func LooksLikeCommitment(userText string) bool {
	lower := " " + strings.ToLower(strings.Join(strings.Fields(userText), " ")) + " "

	for _, phrase := range commitmentPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// commitmentPhrases are first-person future and obligation forms. Padded with
// spaces so " will " does not match "willing" or "goodwill".
var commitmentPhrases = []string{
	" i'll ", " i will ", " i'm going to ", " im going to ", " i am going to ",
	" i plan to ", " i need to ", " i have to ", " i must ", " i should ",
	" i promise ", " i commit ", " i'm committing ", " i intend to ",
	" going to finish ", " going to ship ", " will finish ", " will ship ",
	" by tomorrow ", " by tonight ", " by friday ", " by monday ", " by the end of ",
	" by end of ", " before friday ", " this week i ", " today i ", " tomorrow i ",
	" my goal is ", " the plan is to ", " let me ", " i'm going to ",
}
