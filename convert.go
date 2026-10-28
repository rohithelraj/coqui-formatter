package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options tweak a conversion.
type Options struct {
	Narrator string // narrator voice id; defaults to an existing front matter value or defaultNarrator
	Title    string // overrides the detected title
}

// Report describes what the converter did, so the user knows what to double check.
type Report struct {
	Title      string      `json:"title"`
	Characters []Character `json:"characters"`
	Guesses    []Guess     `json:"guesses"`
	Warnings   []string    `json:"warnings"`
	Words      int         `json:"words"`
}

type Character struct {
	Name   string `json:"name"`
	Voice  string `json:"voice"`
	Gender string `json:"gender"`
	Lines  int    `json:"lines"`
}

// Guess is a dialogue line whose speaker was inferred rather than stated, or left unattributed.
type Guess struct {
	Line    int    `json:"line"` // line in the input
	Speaker string `json:"speaker"`
	Reason  string `json:"reason"`
	Text    string `json:"text"`
}

// ---- regular expressions ------------------------------------------------------------------

const (
	speechVerbs = `said|says|asked|asks|replied|replies|answered|answers|shouted|shouts|yelled|yells|` +
		`cried|cries|called|calls|whispered|whispers|murmured|murmurs|muttered|mutters|added|adds|` +
		`continued|continues|exclaimed|exclaims|snapped|snaps|began|begins|insisted|insists|` +
		`explained|explains|demanded|demands|warned|warns|admitted|admits|agreed|agrees|` +
		`laughed|laughs|sighed|sighs|groaned|groans|growled|growls|hissed|hisses|screamed|screams|` +
		`breathed|stammered|stuttered|repeated|repeats|suggested|suggests|offered|offers|` +
		`protested|declared|announced|interrupted|interrupts|retorted|countered|told|tells|` +
		`wondered|pleaded|begged|urged|ordered|commanded|responded|responds|observed|noted|` +
		`went on|chimed in|spoke|speaks`
	honorifics = `Mr|Mrs|Ms|Miss|Dr|Sir|Lady|Lord|Captain|Capt|Professor|Prof|Aunt|Uncle|King|Queen|` +
		`Prince|Princess|Father|Mother|Sister|Brother|Old|Young|Little|Grandma|Grandpa|Granny|` +
		`Officer|Detective|Inspector|Sergeant|Madame|Monsieur|Master|Mistress|Doctor`
	relatives = `father|mother|brother|sister|uncle|aunt|husband|wife|son|daughter|grandmother|grandfather|` +
		`grandma|grandpa|cousin|friend|boss|teacher|mum|mom|dad|nephew|niece`
	describedNouns = `man|woman|boy|girl|stranger|keeper|captain|doctor|driver|guard|king|queen|` +
		`soldier|child|lady|gentleman|officer|nurse|teacher|waiter|waitress|priest|farmer|` +
		`old man|old woman|innkeeper|landlord|landlady|sailor|knight|witch|wizard|voice|girl|lad`
)

var (
	nameWordRe = `[A-Z][\p{L}'’\-]*`
	nameRe     = `(?:(?:` + honorifics + `)\.?\s+)?` + nameWordRe + `(?:\s+` + nameWordRe + `)?`
	descrRe    = `[Tt]he\s+(?:(?:old|young|little|tall|other|first|second|third|elder|younger)\s+)?(?:` + describedNouns + `)`
	relativeRe = `(?:[Hh]is|[Hh]er|[Tt]heir|[Mm]y|[Oo]ur)\s+(?:` + relatives + `)`
	pronounRe  = `he|she|they|I|He|She|They`
	whoRe      = `(` + descrRe + `|` + relativeRe + `|` + nameRe + `|` + pronounRe + `)`
	verbRe     = `(?:` + speechVerbs + `)`
	advRe      = `(?:\s+[a-z]+ly)?`

	// tag right after a quote: `, said Alice` / `, Alice said quietly`
	tagAfterRe = regexp.MustCompile(`^\s*[,.!?;:—–\-]*\s*(?:` + verbRe + advRe + `\s+` + whoRe + `|` + whoRe + advRe + `\s+` + verbRe + `)(?:[^\p{L}]|$)`)
	// tag right before a quote: `Alice said, ` / `Then Tom asked: `
	tagBeforeRe = regexp.MustCompile(`(?:^|[\s.!?])` + whoRe + advRe + `\s+` + verbRe + advRe + `(?:\s+(?:to|at)\s+(?:him|her|them|me|us))?\s*[,:]?\s*$`)
	// `said Tom, not looking up` -> groups: verb, adverb, who, rest
	verbFirstRe = regexp.MustCompile(`^(` + verbRe + `)(` + advRe + `)\s+` + whoRe + `(.*)$`)
	// narration that is nothing but a speech tag, after trimming punctuation
	bareTagRe = regexp.MustCompile(`^(?:` + verbRe + advRe + `\s+` + whoRe + advRe + `|` + whoRe + advRe + `\s+` + verbRe + advRe +
		`(?:\s+(?:to|at)\s+(?:him|her|them|me|us))?)$`)

	headingRe     = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	sceneBreakRe  = regexp.MustCompile(`^\s*[*\-_#•·⁂§~=+]+(?:\s*[*\-_#•·⁂§~=+]+)*\s*$`)
	setextH1Re    = regexp.MustCompile(`^=+\s*$`)
	setextH2Re    = regexp.MustCompile(`^-+\s*$`)
	fenceRe       = regexp.MustCompile("^\\s*(```|~~~)")
	tableRe       = regexp.MustCompile(`^\s*\|`)
	listRe        = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+\S`)
	quoteLineRe   = regexp.MustCompile(`^\s*>`)
	speakerLineRe = regexp.MustCompile(`^\s*>\s*\*\*([^*:]+?)(?::\*\*|\*\*:)`)
	bookTagRe     = regexp.MustCompile(`(?i)^\s*\[(?:pause\s+[\d.]+\s*(?:ms|s)?|voice:[^\]]*|/voice)\]\s*$`)
	refDefRe      = regexp.MustCompile(`^\s*\[[^\]]+\]:\s+\S`)
	commentRe     = regexp.MustCompile(`(?s)<!--.*?-->`)
	romanRe       = regexp.MustCompile(`(?i)\b(chapter|part|book|act|volume)\s+([IVXLC]+)\b`)

	inlineSubs = []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`), ""},        // images
		{regexp.MustCompile(`\[\^[^\]]+\]`), ""},                // footnote refs
		{regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`), "$1"},     // links
		{regexp.MustCompile(`\[([^\]]+)\]\[[^\]]*\]`), "$1"},    // reference links
		{regexp.MustCompile("`([^`]*)`"), "$1"},                 // inline code
		{regexp.MustCompile(`(?i)<br\s*/?>`), " "},              // line breaks
		{regexp.MustCompile(`</?[a-zA-Z][^>]*>`), ""},           // html tags
		{regexp.MustCompile(`\*\*(.+?)\*\*`), "$1"},             // bold
		{regexp.MustCompile(`__(.+?)__`), "$1"},                 // bold
		{regexp.MustCompile(`\*([^*\s][^*]*?)\*`), "$1"},        // italic
		{regexp.MustCompile(`(^|\W)_([^_]+)_(\W|$)`), "$1$2$3"}, // italic
		{regexp.MustCompile(`~~(.+?)~~`), "$1"},                 // strikethrough
		{regexp.MustCompile(`\s*&\s*`), " and "},
		{regexp.MustCompile(`(\d)\s?%`), "$1 percent"},
		{regexp.MustCompile(`\be\.g\.,?`), "for example"},
		{regexp.MustCompile(`\bi\.e\.,?`), "that is"},
		{regexp.MustCompile(`\s--\s|--`), " — "},
		{regexp.MustCompile(`\s+`), " "},
	}

	// leading words that a two-word capitalised match can pick up but that aren't names
	stopWords = toSet("A An And As At But By For From He Her His How I If In It Its Just Let Me My No Not Now " +
		"Of Oh Ok Okay On Or She So Some Still Than That The Their Them Then There These They This Those " +
		"Though To Too Us We Well What When Where Which While Who Why With Yes Yet You Your Someone " +
		"Somebody Everyone Everybody Nobody Anyone One Another Both Each Even Finally Suddenly Again " +
		"After Before Once Later Soon Today Tonight Tomorrow Yesterday Here God Hello Hi Hey Please Sorry Thanks")
	maleWords   = toSet("mr sir lord king prince father brother uncle grandpa grandfather monsieur master man boy gentleman lad he husband son dad nephew")
	femaleWords = toSet("mrs ms miss lady queen princess mother sister aunt grandma grandmother granny madame mistress woman girl landlady waitress witch she wife daughter mum mom niece")
)

func toSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// ---- blocks ---------------------------------------------------------------------------------

type blockKind int

const (
	paraBlock blockKind = iota
	headingBlock
	sceneBlock
	passBlock // lines copied as they are: lists, blockquotes, book tags
)

type block struct {
	kind  blockKind
	level int    // heading level
	text  string // paragraph / heading text
	lines []string
	line  int // first input line
}

func cleanInline(s string) string {
	for _, sub := range inlineSubs {
		s = sub.re.ReplaceAllString(s, sub.repl)
	}
	return strings.TrimSpace(s)
}

// splitBlocks turns the markdown body into blocks, dropping things that can't be read aloud.
func splitBlocks(body string, firstLine int, warn func(string)) []block {
	var blocks []block
	var para []string
	paraLine := 0
	inFence := false
	skipped := map[string]int{}

	flush := func() {
		if len(para) > 0 {
			blocks = append(blocks, block{kind: paraBlock, text: strings.Join(para, " "), line: paraLine})
			para = nil
		}
	}
	addPass := func(line string, n int) {
		flush()
		if len(blocks) > 0 && blocks[len(blocks)-1].kind == passBlock && blocks[len(blocks)-1].line+len(blocks[len(blocks)-1].lines) == n {
			blocks[len(blocks)-1].lines = append(blocks[len(blocks)-1].lines, line)
			return
		}
		blocks = append(blocks, block{kind: passBlock, lines: []string{line}, line: n})
	}

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		n := firstLine + i
		trimmed := strings.TrimSpace(line)
		switch {
		case fenceRe.MatchString(line):
			flush()
			inFence = !inFence
			if inFence {
				skipped["code blocks"]++
			}
		case inFence:
		case trimmed == "":
			flush()
		case tableRe.MatchString(line):
			flush()
			if i == 0 || !tableRe.MatchString(lines[i-1]) {
				skipped["tables"]++
			}
		case refDefRe.MatchString(line):
			flush()
		case len(para) > 0 && setextH1Re.MatchString(trimmed):
			blocks = append(blocks, block{kind: headingBlock, level: 1, text: strings.Join(para, " "), line: paraLine})
			para = nil
		case len(para) > 0 && setextH2Re.MatchString(trimmed):
			blocks = append(blocks, block{kind: headingBlock, level: 2, text: strings.Join(para, " "), line: paraLine})
			para = nil
		case headingRe.MatchString(trimmed):
			flush()
			m := headingRe.FindStringSubmatch(trimmed)
			blocks = append(blocks, block{kind: headingBlock, level: len(m[1]), text: m[2], line: n})
		case sceneBreakRe.MatchString(trimmed):
			flush()
			blocks = append(blocks, block{kind: sceneBlock, line: n})
		case quoteLineRe.MatchString(line), listRe.MatchString(line), bookTagRe.MatchString(line):
			addPass(line, n)
		default:
			if len(para) == 0 {
				paraLine = n
			}
			para = append(para, trimmed)
		}
	}
	flush()
	for _, what := range []string{"code blocks", "tables"} {
		if skipped[what] > 0 {
			warn(fmt.Sprintf("Removed %d %s (not readable aloud).", skipped[what], what))
		}
	}
	return blocks
}

// ---- quotes ---------------------------------------------------------------------------------

type piece struct {
	quote  bool
	text   string
	closed bool
}

// splitQuotes splits a paragraph into narration and quoted pieces. With single=true, curly single
// quotes (‘…’, British style) mark speech; otherwise double quotes, curly quotes and «guillemets».
func splitQuotes(s string, single bool) []piece {
	var pieces []piece
	var buf strings.Builder
	inQuote := false
	var opener rune
	runes := []rune(s)
	isLetter := func(i int) bool {
		return i >= 0 && i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]))
	}
	emit := func(quote, closed bool) {
		if buf.Len() > 0 || quote {
			pieces = append(pieces, piece{quote: quote, text: buf.String(), closed: closed})
		}
		buf.Reset()
	}
	for i, r := range runes {
		open, close := false, false
		if single {
			switch {
			case r == '‘' && !isLetter(i-1):
				open = !inQuote
			case r == '’' && inQuote && !isLetter(i+1):
				close = true
			}
		} else {
			switch r {
			case '"':
				open, close = !inQuote, inQuote
			case '“':
				open = !inQuote
				close = inQuote && opener == '„'
			case '”', '»':
				close = inQuote
			case '«', '„':
				open = !inQuote
			}
		}
		switch {
		case open:
			emit(false, false)
			inQuote, opener = true, r
		case close:
			emit(true, true)
			inQuote = false
		default:
			buf.WriteRune(r)
		}
	}
	if inQuote {
		emit(true, false)
	} else {
		emit(false, false)
	}
	return pieces
}

func detectSingleQuotes(body string) bool {
	doubles := strings.Count(body, `"`)/2 + strings.Count(body, "“") + strings.Count(body, "«") + strings.Count(body, "„")
	singles := 0
	prev := ' '
	for _, r := range body {
		if r == '‘' && !unicode.IsLetter(prev) {
			singles++
		}
		prev = r
	}
	return singles > doubles
}

// ---- speakers -------------------------------------------------------------------------------

type speaker struct {
	name   string
	gender string
	lines  int
	order  int
	re     *regexp.Regexp // matches the name as a whole word
}

type cast struct {
	byName map[string]*speaker
}

func (c *cast) get(name string) *speaker {
	if s, ok := c.byName[name]; ok {
		return s
	}
	s := &speaker{name: name, order: len(c.byName),
		re: regexp.MustCompile(`(?:^|[^\p{L}])` + regexp.QuoteMeta(name) + `(?:[^\p{L}]|$)`)}
	c.byName[name] = s
	return s
}

func (c *cast) setGender(name, gender string) {
	if gender != "" {
		if s := c.get(name); s.gender == "" {
			s.gender = gender
		}
	}
}

// normalizeWho turns a tag subject into a character name, or returns a pronoun marker.
// Returns (name, gender, isPronoun).
func normalizeWho(who string) (string, string, bool) {
	switch strings.ToLower(who) {
	case "he":
		return "", "male", true
	case "she":
		return "", "female", true
	case "they":
		return "", "", true
	case "i":
		return "narrator", "", false
	}
	words := strings.Fields(who)
	switch strings.ToLower(words[0]) {
	case "the", "his", "her", "their", "my", "our":
		// "the old man" -> "Old Man", "her father" -> "Father"
		words = words[1:]
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	for len(words) > 0 && stopWords[words[0]] {
		words = words[1:]
	}
	if len(words) == 0 {
		return "", "", false
	}
	gender := ""
	for _, w := range words {
		w = strings.ToLower(strings.TrimSuffix(w, "."))
		if maleWords[w] {
			gender = "male"
		} else if femaleWords[w] {
			gender = "female"
		}
	}
	return strings.Join(words, " "), gender, false
}

type tag struct {
	name    string
	gender  string
	pronoun bool
}

func findTag(re *regexp.Regexp, s string) *tag {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	for _, who := range m[1:] {
		if who == "" {
			continue
		}
		name, gender, pronoun := normalizeWho(who)
		if name == "" && !pronoun {
			return nil
		}
		return &tag{name: name, gender: gender, pronoun: pronoun}
	}
	return nil
}

func isBareTag(s string) bool {
	s = strings.Trim(s, " ,.;:!?—–-")
	return s == "" || bareTagRe.MatchString(s)
}

// looksLikeDialogue filters out scare quotes and titles: `the so-called "expert"`.
func looksLikeDialogue(p piece, idx int, pieces []piece, tagged bool) bool {
	if tagged {
		return true
	}
	text := strings.TrimSpace(p.text)
	if idx == 0 || (idx == 1 && strings.TrimSpace(pieces[0].text) == "") {
		return text != ""
	}
	if !p.closed {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	return strings.ContainsRune(",.!?…—–-", last) && len(strings.Fields(text)) >= 2
}

// ---- conversion -----------------------------------------------------------------------------

type frontMatter struct {
	title, author string
	voices        [][2]string // keeps order
	genders       map[string]string
	pauses        []string // raw lines of the pauses block
}

var fmLineRe = regexp.MustCompile(`^([A-Za-z_]+):\s*(.*?)\s*$`)
var fmItemRe = regexp.MustCompile(`^\s+(.+?):\s*([^#]*?)\s*(?:#\s*(male|female)?.*)?$`)

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// parseFrontMatter reads the few keys we care about with a tiny YAML subset parser.
func parseFrontMatter(md string) (frontMatter, string, int) {
	var fm frontMatter
	if !strings.HasPrefix(md, "---\n") {
		return fm, md, 1
	}
	end := strings.Index(md[4:], "\n---")
	if end < 0 {
		return fm, md, 1
	}
	raw := md[4 : 4+end]
	if !fmLineRe.MatchString(strings.SplitN(raw, "\n", 2)[0]) {
		return fm, md, 1 // a leading `---` scene break, not front matter
	}
	rest := md[4+end+4:]
	if i := strings.Index(rest, "\n"); i >= 0 {
		rest = rest[i+1:]
	} else {
		rest = ""
	}
	section := ""
	for _, line := range strings.Split(raw, "\n") {
		if m := fmLineRe.FindStringSubmatch(line); m != nil {
			section = ""
			switch m[1] {
			case "title":
				fm.title = unquote(m[2])
			case "author":
				fm.author = unquote(m[2])
			case "voices", "pauses":
				section = m[1]
			}
			continue
		}
		if m := fmItemRe.FindStringSubmatch(line); m != nil {
			switch section {
			case "voices":
				name := unquote(m[1])
				fm.voices = append(fm.voices, [2]string{name, unquote(m[2])})
				if m[3] != "" {
					if fm.genders == nil {
						fm.genders = map[string]string{}
					}
					fm.genders[name] = m[3]
				}
			case "pauses":
				fm.pauses = append(fm.pauses, "  "+strings.TrimSpace(line))
			}
		}
	}
	return fm, rest, strings.Count(md[:len(md)-len(rest)], "\n") + 1
}

func romanToInt(s string) int {
	vals := map[rune]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100}
	total, prev := 0, 0
	runes := []rune(strings.ToUpper(s))
	for i := len(runes) - 1; i >= 0; i-- {
		v := vals[runes[i]]
		if v < prev {
			total -= v
		} else {
			total += v
			prev = v
		}
	}
	return total
}

var smallNumbers = strings.Fields("zero one two three four five six seven eight nine ten eleven twelve thirteen fourteen " +
	"fifteen sixteen seventeen eighteen nineteen")
var tens = strings.Fields("_ _ twenty thirty forty fifty sixty seventy eighty ninety")

func numberWords(n int) string {
	switch {
	case n < 20:
		return smallNumbers[n]
	case n < 100:
		if n%10 == 0 {
			return tens[n/10]
		}
		return tens[n/10] + "-" + smallNumbers[n%10]
	}
	return fmt.Sprint(n)
}

func headingText(s string) string {
	s = cleanInline(s)
	return romanRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := romanRe.FindStringSubmatch(m)
		n := romanToInt(parts[2])
		if n <= 0 || n >= 100 {
			return m
		}
		w := numberWords(n)
		return parts[1] + " " + strings.ToUpper(w[:1]) + w[1:]
	})
}

type item struct {
	speaker string // "" = narration
	text    string
}

type converter struct {
	cast         *cast
	single       bool
	report       *Report
	history      []string // speakers of consecutive dialogue paragraphs, reset at scene changes
	prevDialogue bool     // previous paragraph contained dialogue
	openSpeaker  string   // speaker of a quote left open at the end of the previous paragraph
}

func (c *converter) guess(line int, speaker, reason, text string) {
	text = strings.TrimSpace(text)
	if len(text) > 80 {
		text = text[:77] + "..."
	}
	c.report.Guesses = append(c.report.Guesses, Guess{Line: line, Speaker: speaker, Reason: reason, Text: text})
}

// knownInText returns the first known character named in s.
func (c *converter) knownInText(s string, gender string) string {
	best, bestPos := "", -1
	for name, sp := range c.cast.byName {
		if name == "narrator" || (gender != "" && sp.gender != "" && sp.gender != gender) {
			continue
		}
		if loc := sp.re.FindStringIndex(s); loc != nil && (bestPos < 0 || loc[0] < bestPos) {
			best, bestPos = name, loc[0]
		}
	}
	return best
}

func (c *converter) lastSpeaker(gender string, exclude string) string {
	for i := len(c.history) - 1; i >= 0; i-- {
		name := c.history[i]
		if name == exclude || name == "narrator" {
			continue
		}
		if gender == "" || c.cast.get(name).gender == gender || c.cast.get(name).gender == "" {
			return name
		}
	}
	return ""
}

func (c *converter) resolvePronoun(t *tag, narration string, line int, text string) string {
	if name := c.knownInText(narration, t.gender); name != "" {
		c.cast.setGender(name, t.gender)
		return name
	}
	if name := c.lastSpeaker(t.gender, ""); name != "" {
		c.cast.setGender(name, t.gender)
		c.guess(line, name, "pronoun → most recent matching speaker", text)
		return name
	}
	return ""
}

// paragraph converts one paragraph into narration and dialogue items.
func (c *converter) paragraph(b block) []item {
	text := cleanInline(b.text)
	pieces := splitQuotes(text, c.single)
	narration := ""
	for _, p := range pieces {
		if !p.quote {
			narration += p.text + " "
		}
	}

	speakers := make([]string, len(pieces))
	dialogue := make([]bool, len(pieces))
	paraSpeaker := ""
	for i, p := range pieces {
		if !p.quote {
			continue
		}
		var t *tag
		if i+1 < len(pieces) && !pieces[i+1].quote {
			t = findTag(tagAfterRe, pieces[i+1].text)
		}
		if t == nil && i > 0 && !pieces[i-1].quote {
			t = findTag(tagBeforeRe, pieces[i-1].text)
		}
		if !looksLikeDialogue(p, i, pieces, t != nil) {
			continue
		}
		dialogue[i] = true
		switch {
		case t != nil && t.pronoun:
			speakers[i] = c.resolvePronoun(t, narration, b.line, p.text)
		case t != nil:
			speakers[i] = t.name
			c.cast.setGender(t.name, t.gender)
		}
		if speakers[i] != "" && paraSpeaker == "" {
			paraSpeaker = speakers[i]
		}
	}

	// untagged quotes: same paragraph speaker, open quote continuation, action beat, turn-taking
	hasDialogue := false
	for i := range pieces {
		if !dialogue[i] {
			continue
		}
		hasDialogue = true
		if speakers[i] != "" {
			continue
		}
		p := pieces[i]
		switch {
		case paraSpeaker != "":
			speakers[i] = paraSpeaker
		case i <= 1 && c.openSpeaker != "":
			speakers[i] = c.openSpeaker
		default:
			if name := c.knownInText(narration, ""); name != "" {
				speakers[i] = name
				c.guess(b.line, name, "character named in the same paragraph", p.text)
			} else if c.prevDialogue && len(c.history) >= 2 && c.history[len(c.history)-1] != c.history[len(c.history)-2] {
				speakers[i] = c.history[len(c.history)-2]
				c.guess(b.line, speakers[i], "turn-taking (alternating speakers)", p.text)
			} else if prev := c.lastSpeaker("", ""); c.prevDialogue && prev != "" {
				if other := c.lastSpeaker("", prev); other != "" {
					speakers[i] = other
					c.guess(b.line, other, "turn-taking (reply to "+prev+")", p.text)
				}
			}
			if speakers[i] == "" {
				dialogue[i] = false
				c.guess(b.line, "", "no speaker found, left as narration", p.text)
			}
		}
		paraSpeaker = speakers[i]
	}

	// build items, dropping bare speech tags from the narration
	var items []item
	var narr strings.Builder
	flushNarr := func() {
		s := strings.TrimSpace(narr.String())
		narr.Reset()
		if isBareTag(s) {
			return
		}
		s = strings.TrimLeft(s, " ,;:—–-")
		if m := verbFirstRe.FindStringSubmatch(s); m != nil {
			s = m[3] + " " + m[1] + m[2] + m[4]
		}
		r, size := utf8.DecodeRuneInString(s)
		s = string(unicode.ToUpper(r)) + s[size:]
		if last, _ := utf8.DecodeLastRuneInString(s); !strings.ContainsRune(".!?…\"”’)", last) {
			s = strings.TrimRight(s, ",;: ") + "."
		}
		items = append(items, item{text: s})
	}
	c.openSpeaker = ""
	for i, p := range pieces {
		switch {
		case !p.quote:
			narr.WriteString(p.text)
		case !dialogue[i]:
			narr.WriteString(`"` + p.text + `"`)
		case speakers[i] == "narrator":
			// first-person narrator ("…," I said): the narrator voice reads it as a sentence
			flushNarr()
			narr.WriteString(strings.TrimSpace(p.text))
			flushNarr()
		default:
			flushNarr()
			line := strings.TrimSpace(p.text)
			if strings.HasSuffix(line, ",") {
				line = strings.TrimSuffix(line, ",") + "."
			}
			if n := len(items); n > 0 && items[n-1].speaker == speakers[i] {
				items[n-1].text += " " + line
			} else if line != "" {
				items = append(items, item{speaker: speakers[i], text: line})
			}
			if !p.closed {
				c.openSpeaker = speakers[i]
			}
		}
	}
	flushNarr()

	// remember who spoke for turn-taking
	if hasDialogue {
		for _, it := range items {
			if it.speaker != "" {
				c.cast.get(it.speaker).lines++
				if len(c.history) == 0 || c.history[len(c.history)-1] != it.speaker {
					c.history = append(c.history, it.speaker)
				}
			}
		}
	}
	c.prevDialogue = false
	for _, it := range items {
		if it.speaker != "" {
			c.prevDialogue = true
		}
	}
	return items
}

var (
	sentenceRe = regexp.MustCompile(`[^.!?]+[.!?]*`)
	maleRe     = regexp.MustCompile(`\b(?:he|his|him|himself)\b`)
	femaleRe   = regexp.MustCompile(`\b(?:she|her|hers|herself)\b`)
)

// inferGenders votes on the gender of characters whose gender is still unknown, using he/his and
// she/her in narration sentences that mention exactly one character ("Tom closed his book.").
func (c *converter) inferGenders(blocks []block) {
	votes := map[string]int{} // >0 male, <0 female
	for _, b := range blocks {
		if b.kind != paraBlock {
			continue
		}
		var narration strings.Builder
		for _, p := range splitQuotes(cleanInline(b.text), c.single) {
			if !p.quote {
				narration.WriteString(p.text + " ")
			}
		}
		for _, sentence := range sentenceRe.FindAllString(narration.String(), -1) {
			var named *speaker
			count := 0
			for _, sp := range c.cast.byName {
				if sp.name != "narrator" && sp.re.MatchString(sentence) {
					named, count = sp, count+1
				}
			}
			if count != 1 || named.gender != "" {
				continue
			}
			rest := sentence[named.re.FindStringIndex(sentence)[1]-1:]
			votes[named.name] += len(maleRe.FindAllString(rest, -1)) - len(femaleRe.FindAllString(rest, -1))
		}
	}
	for name, v := range votes {
		if v > 0 {
			c.cast.setGender(name, "male")
		} else if v < 0 {
			c.cast.setGender(name, "female")
		}
	}
}

// promoteHeadings handles the common "one # title, ## chapters" layout. The TTS server starts a
// chapter (M4B marker) at every #, so ## chapters are promoted to # and the rest shift up too.
func promoteHeadings(blocks []block) {
	h1, h2, firstH2 := 0, 0, -1
	for i, b := range blocks {
		if b.kind != headingBlock {
			continue
		}
		switch b.level {
		case 1:
			h1++
			if firstH2 >= 0 {
				return // a # after the chapters: the author's structure, leave it alone
			}
		case 2:
			h2++
			if firstH2 < 0 {
				firstH2 = i
			}
		}
	}
	if h1 > 1 || h2 < 2 {
		return
	}
	for i := range blocks {
		if blocks[i].kind == headingBlock && blocks[i].level > 1 {
			blocks[i].level--
		}
	}
}

func (c *converter) resetScene() {
	c.history = nil
	c.prevDialogue = false
	c.openSpeaker = ""
}

func yamlKey(s string) string {
	switch strings.ToLower(s) {
	case "yes", "no", "on", "off", "true", "false", "null", "y", "n", "~":
		return `"` + s + `"`
	}
	if strings.ContainsAny(s, `:#'"{}[],&*!|>%@`+"`") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// Convert turns arbitrary markdown into Book Markdown for the coqui TTS audiobook server.
func Convert(md string, opts Options) (string, Report) {
	report := Report{}
	md = strings.TrimPrefix(strings.ReplaceAll(md, "\r\n", "\n"), "\uFEFF")
	fm, body, firstLine := parseFrontMatter(md)
	// drop comments but keep line numbers
	body = commentRe.ReplaceAllStringFunc(body, func(m string) string { return strings.Repeat("\n", strings.Count(m, "\n")) })

	c := &converter{cast: &cast{byName: map[string]*speaker{}}, single: detectSingleQuotes(body), report: &report}
	warn := func(s string) { report.Warnings = append(report.Warnings, s) }
	blocks := splitBlocks(body, firstLine, warn)

	// existing voices and `> **Name:**` lines are characters we already know
	narrator := opts.Narrator
	for _, v := range fm.voices {
		if v[0] == "narrator" {
			if narrator == "" {
				narrator = v[1]
			}
			continue
		}
		c.cast.get(v[0])
		c.cast.setGender(v[0], fm.genders[v[0]])
	}
	if narrator == "" {
		narrator = defaultNarrator
	}
	// first pass: names from explicit speech tags, so action beats and pronouns can refer to them
	for _, b := range blocks {
		if b.kind != paraBlock {
			continue
		}
		pieces := splitQuotes(cleanInline(b.text), c.single)
		for i, p := range pieces {
			if p.quote || i == 0 {
				continue
			}
			if t := findTag(tagAfterRe, p.text); t != nil && !t.pronoun && t.name != "narrator" {
				c.cast.get(t.name)
				c.cast.setGender(t.name, t.gender)
			}
		}
	}
	for _, b := range blocks {
		for _, line := range b.lines {
			if m := speakerLineRe.FindStringSubmatch(line); m != nil {
				c.cast.get(strings.TrimSpace(m[1])).lines++
			}
		}
	}

	c.inferGenders(blocks)
	promoteHeadings(blocks)

	title := opts.Title
	if title == "" {
		title = fm.title
	}
	var out []string // output blocks
	lastWasDialogue := false
	appendBlock := func(s string, dialogue bool) {
		if dialogue && lastWasDialogue && len(out) > 0 {
			out[len(out)-1] += "\n" + s
		} else {
			out = append(out, s)
		}
		lastWasDialogue = dialogue
	}

	for _, b := range blocks {
		switch b.kind {
		case headingBlock:
			text := headingText(b.text)
			if text == "" {
				continue
			}
			if title == "" && b.level == 1 {
				title = text
			}
			c.resetScene()
			appendBlock(strings.Repeat("#", b.level)+" "+text, false)
		case sceneBlock:
			c.resetScene()
			if len(out) > 0 && out[len(out)-1] != "---" {
				appendBlock("---", false)
			}
		case passBlock:
			c.prevDialogue = false
			appendBlock(strings.Join(b.lines, "\n"), false)
		case paraBlock:
			for _, it := range c.paragraph(b) {
				if it.speaker == "" {
					appendBlock(it.text, false)
				} else {
					appendBlock("> **"+it.speaker+":** "+it.text, true)
				}
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == "---" {
		out = out[:len(out)-1]
	}
	if title == "" {
		title = "Untitled"
	}
	report.Title = title

	// voices: keep existing assignments, give everyone else a distinct voice
	existing := map[string]string{}
	taken := []string{narrator}
	for _, v := range fm.voices {
		existing[v[0]] = v[1]
		taken = append(taken, v[1])
	}
	assign := newVoiceAssigner(taken...)
	var speakers []*speaker
	for _, s := range c.cast.byName {
		if s.name == "narrator" || (s.lines == 0 && existing[s.name] == "") {
			continue
		}
		speakers = append(speakers, s)
	}
	sort.Slice(speakers, func(i, j int) bool { return speakers[i].order < speakers[j].order })

	var fmOut strings.Builder
	fmOut.WriteString("---\n")
	fmOut.WriteString("title: " + yamlValue(title) + "\n")
	if fm.author != "" {
		fmOut.WriteString("author: " + yamlValue(fm.author) + "\n")
	}
	fmOut.WriteString("voices:\n")
	fmOut.WriteString("  narrator: " + narrator + "\n")
	for _, s := range speakers {
		voice := existing[s.name]
		if voice == "" {
			voice = assign.next(s.gender)
		}
		comment := s.gender
		if comment == "" {
			comment = "gender unknown"
		}
		fmOut.WriteString(fmt.Sprintf("  %s: %s  # %s, %s\n", yamlKey(s.name), voice, comment, plural(s.lines, "line")))
		report.Characters = append(report.Characters, Character{Name: s.name, Voice: voice, Gender: s.gender, Lines: s.lines})
	}
	if len(fm.pauses) > 0 {
		fmOut.WriteString("pauses:\n" + strings.Join(fm.pauses, "\n") + "\n")
	}
	fmOut.WriteString("---\n")

	bodyOut := strings.Join(out, "\n\n")
	report.Words = len(strings.Fields(bodyOut))
	if report.Words == 0 {
		warn("No readable text found.")
	}
	if len(report.Characters) == 0 {
		warn("No dialogue found: everything will be read by the narrator.")
	}
	if c.single {
		warn("Detected British-style ‘single quotes’ for dialogue.")
	}
	sort.SliceStable(report.Guesses, func(i, j int) bool { return report.Guesses[i].Line < report.Guesses[j].Line })
	return fmOut.String() + "\n" + bodyOut + "\n", report
}

func yamlValue(s string) string {
	if strings.ContainsAny(s, `:#'"{}[]&*!|>%@`+"`") || strings.TrimSpace(s) != s {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
