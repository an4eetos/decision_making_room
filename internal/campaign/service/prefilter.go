// Package service holds the campaign's pure logic: the prefilter that decides
// whether a turn is worth an extraction call, parsing what the model returns,
// and reading intel straight out of an interrogation without a model at all.
package service

import (
	"strings"

	comservice "github.com/an4eetos/decision-room/internal/commitments/service"
)

// Fingerprint is shared with open loops: the same goal said twice in different
// words is one row, by the same rule.
func Fingerprint(text string) string { return comservice.Fingerprint(text) }

// LooksLikeIntel is the free check run before paying for extraction. Most turns
// carry no goal, blocker or open question, and stop here.
//
// It reads only what you wrote. The assistant's answer is full of goals and
// obstacles it suggested, and mapping those would fill the campaign with things
// you never said.
func LooksLikeIntel(userText string) bool {
	lower := " " + strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(userText, "’", "'")), " ")) + " "
	for _, group := range [][]string{objectivePhrases, obstaclePhrases, unknownPhrases} {
		for _, phrase := range group {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	return false
}

// Padded with spaces where a bare word would match inside another one.
var objectivePhrases = []string{
	" i want to ", " i'd like to ", " i would like to ", " my goal ", " goal is ",
	" i aim to ", " aiming to ", " i'm trying to ", " i am trying to ", " hoping to ",
	" i plan to ", " the plan is ", " objective ", " this year i ", " by next year ",
	" i dream of ", " i need to get ", " want to become ", " want to move ", " want to build ",
	" want to launch ", " want to quit ", " want to finish ",
}

var obstaclePhrases = []string{
	" stuck ", " blocked ", " blocker ", " can't because ", " cannot because ",
	" waiting on ", " waiting for ", " afraid ", " scared ", " worried ", " problem is ",
	" the issue is ", " struggling ", " too big ", " overwhelm", " don't want to ",
	" dreading ", " procrastinat", " avoiding ", " can't start ", " can't get ",
	" keeps getting ", " in the way ", " holding me back ", " no time ", " no money ",
}

var unknownPhrases = []string{
	" not sure ", " don't know ", " do not know ", " unclear ", " no idea ",
	" wondering ", " what if ", " whether ", " uncertain ", " can't tell ",
	" find out ", " figure out ", " no clue ", " depends on ",
}
