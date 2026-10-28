package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func body(md string) string {
	// drop the front matter
	parts := strings.SplitN(md, "\n---\n", 2)
	return strings.TrimSpace(parts[len(parts)-1])
}

func TestTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"Hello," said Alice.`, "> **Alice:** Hello."},
		{`"Hello," Alice said.`, "> **Alice:** Hello."},
		{`"Hello," Alice said quietly.`, "> **Alice:** Hello."},
		{`Alice said, "Hello."`, "> **Alice:** Hello."},
		{`"Hello," said the old man.`, "> **Old Man:** Hello."},
		{`"Hello," her father replied.`, "> **Father:** Hello."},
		{`"Hello," said Mrs. Hart.`, "> **Mrs. Hart:** Hello."},
		{`“Hello,” said Alice.`, "> **Alice:** Hello."},
		{`"Wait," Tom said. "Don't go."`, "> **Tom:** Wait. Don't go."},
		{`"Wait," said Tom, grabbing her arm.`, "> **Tom:** Wait.\n\nTom said, grabbing her arm."},
	}
	for _, c := range cases {
		out, _ := Convert(c.in, Options{})
		if got := body(out); got != c.want {
			t.Errorf("%s\n got: %q\nwant: %q", c.in, got, c.want)
		}
	}
}

func TestTurnTakingAndGuesses(t *testing.T) {
	in := `"Do you hear that?" Alice asked.

"It's only the sea," said Tom.

"No, listen."

"I'm listening."`
	out, report := Convert(in, Options{})
	want := "> **Alice:** Do you hear that?\n> **Tom:** It's only the sea.\n> **Alice:** No, listen.\n> **Tom:** I'm listening."
	if got := body(out); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if len(report.Guesses) != 2 || report.Guesses[0].Speaker != "Alice" || report.Guesses[0].Line != 5 {
		t.Fatalf("unexpected guesses: %+v", report.Guesses)
	}
}

func TestUnattributedStaysNarration(t *testing.T) {
	out, report := Convert(`"Who's there?"`, Options{})
	if got := body(out); got != `"Who's there?"` {
		t.Fatalf("got %q", got)
	}
	if len(report.Guesses) != 1 || report.Guesses[0].Speaker != "" {
		t.Fatalf("expected one unattributed guess, got %+v", report.Guesses)
	}
}

func TestPronounResolution(t *testing.T) {
	in := `"Hi," said Alice.

Alice frowned. "Why?" she asked.`
	out, _ := Convert(in, Options{})
	if !strings.Contains(out, "> **Alice:** Why?") {
		t.Fatalf("pronoun not resolved:\n%s", out)
	}
	if !strings.Contains(out, "Alice: p225  # female") {
		t.Fatalf("gender not learned:\n%s", out)
	}
}

func TestScareQuotesAreNotDialogue(t *testing.T) {
	out, _ := Convert(`The so-called "expert" was wrong.`, Options{})
	if got := body(out); got != `The so-called "expert" was wrong.` {
		t.Fatalf("got %q", got)
	}
}

func TestStructure(t *testing.T) {
	in := "Title\n=====\n\nIntro.\n\n## Chapter IV\n\ntext\n\n* * *\n\n```\ncode\n```\n\n| a |\n|---|\n\n" +
		"![img](x.png)\n\n- item one\n- item two\n\n<!-- note -->\n\nA [link](http://x) & 5% more.[^1]\n\n[^1]: footnote\n"
	out, report := Convert(in, Options{})
	want := "# Title\n\nIntro.\n\n## Chapter Four\n\nText.\n\n---\n\n- item one\n- item two\n\nA link and 5 percent more."
	if got := body(out); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if report.Title != "Title" || !strings.HasPrefix(out, "---\ntitle: Title\n") {
		t.Fatalf("title not detected: %q", report.Title)
	}
	if len(report.Warnings) == 0 {
		t.Fatal("expected warnings for removed code/table")
	}
}

func TestBritishQuotesAndFirstPerson(t *testing.T) {
	in := "‘I shan’t go,’ said Margaret.\n\n‘You must,’ I said."
	out, _ := Convert(in, Options{})
	want := "> **Margaret:** I shan’t go.\n\nYou must."
	if got := body(out); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFrontMatterKeptAndIdempotent(t *testing.T) {
	in := "---\ntitle: \"Book: One\"\nauthor: Jane\nvoices:\n  narrator: p240\n  Alice: p999  # female\n" +
		"pauses:\n  paragraph: 1.0\n---\n\n\"Hi,\" said Alice.\n\n\"Hey,\" said Bob.\n"
	out, report := Convert(in, Options{})
	for _, want := range []string{`title: "Book: One"`, "author: Jane", "narrator: p240", "Alice: p999  # female", "pauses:\n  paragraph: 1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if len(report.Characters) != 2 || report.Characters[1].Name != "Bob" || report.Characters[1].Voice == "p240" {
		t.Errorf("unexpected characters: %+v", report.Characters)
	}
	again, _ := Convert(out, Options{})
	if again != out {
		t.Errorf("second conversion changed the output:\n%s\n---\n%s", out, again)
	}
}

func TestYAMLKeys(t *testing.T) {
	out, _ := Convert(`"Hi," said O'Brien.`, Options{})
	if !strings.Contains(out, `"O'Brien": `) {
		t.Fatalf("name with quote not quoted:\n%s", out)
	}
}

func TestDistinctVoices(t *testing.T) {
	var in strings.Builder
	for _, n := range []string{"Ann", "Bea", "Cal", "Dan", "Eve"} {
		in.WriteString(`"Hi," said ` + n + ".\n\n")
	}
	_, report := Convert(in.String(), Options{Narrator: "p225"})
	seen := map[string]bool{"p225": true}
	for _, c := range report.Characters {
		if seen[c.Voice] {
			t.Fatalf("voice %s used twice: %+v", c.Voice, report.Characters)
		}
		seen[c.Voice] = true
	}
}

func TestOutputName(t *testing.T) {
	for in, want := range map[string]string{"ch1.md": "ch1.book.md", "ch1.book.md": "ch1.book.md", "notes.txt": "notes.book.md"} {
		if got := outputName(in); got != want {
			t.Errorf("outputName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSampleStory(t *testing.T) {
	data, err := os.ReadFile("testdata/story.md")
	if err != nil {
		t.Fatal(err)
	}
	out, report := Convert(string(data), Options{})
	for _, want := range []string{"> **Old Man:** That's the bell. Nobody has rung it in forty years.", "\n# Chapter Two\n", "Tom: p226  # male"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if len(report.Characters) != 4 {
		t.Errorf("expected 4 characters, got %+v", report.Characters)
	}
}

func TestConvertHandler(t *testing.T) {
	srv := &server{}
	req := httptest.NewRequest(http.MethodPost, "/api/convert?name=ch1.md", strings.NewReader(`"Hi," said Alice.`))
	rec := httptest.NewRecorder()
	srv.convert(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Markdown string `json:"markdown"`
		Filename string `json:"filename"`
		Report   Report `json:"report"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Filename != "ch1.book.md" || !strings.Contains(resp.Markdown, "> **Alice:** Hi.") || len(resp.Report.Characters) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	rec = httptest.NewRecorder()
	srv.convert(rec, httptest.NewRequest(http.MethodPost, "/api/convert?format=md", strings.NewReader("Hello.")))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("format=md returned %q", ct)
	}

	rec = httptest.NewRecorder()
	srv.send(rec, httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader("x")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("send without -tts should fail, got %d", rec.Code)
	}
}

func TestPromoteHeadings(t *testing.T) {
	out, _ := Convert("# Book\n\n## One\n\nA.\n\n### Part\n\nB.\n\n## Two\n\nC.", Options{})
	want := "# Book\n\n# One\n\nA.\n\n## Part\n\nB.\n\n# Two\n\nC."
	if got := body(out); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// several # chapters already: untouched
	in := "# One\n\n## a\n\nA.\n\n# Two\n\n## b\n\nB."
	if out, _ := Convert(in, Options{}); body(out) != in {
		t.Fatalf("changed structure:\n%s", body(out))
	}
}
