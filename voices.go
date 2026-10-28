package main

// Speaker ids of the coqui `tts_models/en/vctk/vits` model, grouped by the gender given in the
// VCTK speaker info. Keep in sync with the voice table in TTS/server/BOOK_FORMAT.md.
var (
	femaleVoices = []string{
		"p225", "p228", "p229", "p231", "p233", "p236", "p239", "p240", "p244", "p250", "p257", "p268",
		"p276", "p267", "p269", "p253", "p262", "p266", "p234", "p238", "p249", "p261", "p264", "p248",
	}
	maleVoices = []string{
		"p226", "p232", "p254", "p258", "p227", "p243", "p256", "p259", "p270", "p273", "p274", "p278",
		"p279", "p245", "p252", "p237", "p241", "p246", "p247", "p260", "p263", "p271", "p272", "p275", "p251",
	}
	defaultNarrator = "p230"
)

// voiceAssigner hands out distinct voices, never reusing the narrator's or one already taken.
type voiceAssigner struct {
	used map[string]bool
}

func newVoiceAssigner(taken ...string) *voiceAssigner {
	a := &voiceAssigner{used: map[string]bool{}}
	for _, v := range taken {
		a.used[v] = true
	}
	return a
}

func (a *voiceAssigner) next(gender string) string {
	pools := [][]string{femaleVoices, maleVoices}
	switch gender {
	case "male":
		pools = [][]string{maleVoices, femaleVoices}
	case "":
		// unknown gender: take from whichever pool has handed out fewer voices
		if a.count(maleVoices) < a.count(femaleVoices) {
			pools = [][]string{maleVoices, femaleVoices}
		}
	}
	for _, pool := range pools {
		for _, v := range pool {
			if !a.used[v] {
				a.used[v] = true
				return v
			}
		}
	}
	// more characters than voices: start sharing
	return pools[0][len(a.used)%len(pools[0])]
}

func (a *voiceAssigner) count(pool []string) int {
	n := 0
	for _, v := range pool {
		if a.used[v] {
			n++
		}
	}
	return n
}
