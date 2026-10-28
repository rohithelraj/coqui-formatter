// coqui-formatter converts ordinary markdown into Book Markdown for the coqui TTS audiobook
// server. It runs as a small web server (default) or as a command line tool.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

const maxUpload = 10 << 20 // 10 MB

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "convert" {
		os.Exit(runConvert(os.Args[2:]))
	}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "address to listen on")
	tts := fs.String("tts", "", "URL of the coqui TTS server (e.g. http://tts-pi.local:5002) to enable \"Send to TTS\"")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "coqui-formatter %s\n\nUsage:\n  coqui-formatter [serve] [-addr :8080] [-tts http://tts-pi.local:5002]\n"+
			"  coqui-formatter convert [-narrator p230] [-title T] [-o out.book.md] file.md\n\nServe flags:\n", version)
		fs.PrintDefaults()
	}
	fs.Parse(args)

	srv := &server{tts: strings.TrimRight(*tts, "/"), client: &http.Client{Timeout: 60 * time.Second}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.index)
	mux.HandleFunc("/api/convert", srv.convert)
	mux.HandleFunc("/api/send", srv.send)
	mux.HandleFunc("/api/config", srv.config)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok\n") })

	log.Printf("coqui-formatter %s listening on %s", version, *addr)
	if srv.tts != "" {
		log.Printf("sending books to %s", srv.tts)
	}
	log.Fatal(http.ListenAndServe(*addr, logRequests(mux)))
}

func runConvert(args []string) int {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	narrator := fs.String("narrator", "", "narrator voice id (default p230, or the file's existing narrator)")
	title := fs.String("title", "", "book title (default: front matter title or first # heading)")
	out := fs.String("o", "", "output file, - for stdout (default: <name>.book.md next to the input)")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: coqui-formatter convert [-narrator p230] [-title T] [-o out.book.md] file.md")
		return 2
	}
	in := fs.Arg(0)
	data, err := os.ReadFile(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	md, report := Convert(string(data), Options{Narrator: *narrator, Title: *title})
	if *out == "" {
		*out = outputName(in)
	}
	if *out == "-" {
		os.Stdout.WriteString(md)
	} else if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	} else {
		fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
	}
	printReport(os.Stderr, report)
	return 0
}

func printReport(w io.Writer, r Report) {
	fmt.Fprintf(w, "%s: %d words\n", r.Title, r.Words)
	for _, c := range r.Characters {
		gender := c.Gender
		if gender == "" {
			gender = "?"
		}
		fmt.Fprintf(w, "  %-20s %s  (%s, %d lines)\n", c.Name, c.Voice, gender, c.Lines)
	}
	for _, g := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", g)
	}
	if len(r.Guesses) > 0 {
		fmt.Fprintf(w, "check these %d lines:\n", len(r.Guesses))
		for _, g := range r.Guesses {
			who := g.Speaker
			if who == "" {
				who = "narration"
			}
			fmt.Fprintf(w, "  line %d → %s (%s): %q\n", g.Line, who, g.Reason, g.Text)
		}
	}
}

// outputName maps chapter1.md -> chapter1.book.md.
func outputName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.HasSuffix(base, ".book") {
		return base + ".md"
	}
	if base == "" {
		base = "book"
	}
	return base + ".book.md"
}

// ---- web server ------------------------------------------------------------------------------

type server struct {
	tts    string
	client *http.Client
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}

func (s *server) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tts":      s.tts,
		"narrator": defaultNarrator,
		"female":   femaleVoices,
		"male":     maleVoices,
		"version":  version,
	})
}

// readMarkdown accepts a multipart `file`, a form field `text`, or the raw request body.
func readMarkdown(r *http.Request) (string, string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxUpload)
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(maxUpload); err != nil {
			return "", "", err
		}
		if f, h, err := r.FormFile("file"); err == nil {
			defer f.Close()
			data, err := io.ReadAll(f)
			return string(data), h.Filename, err
		}
		return r.FormValue("text"), r.FormValue("name"), nil
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		if err := r.ParseForm(); err != nil {
			return "", "", err
		}
		return r.FormValue("text"), r.FormValue("name"), nil
	default:
		data, err := io.ReadAll(r.Body)
		return string(data), r.URL.Query().Get("name"), err
	}
}

func (s *server) convert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST markdown to this endpoint"})
		return
	}
	md, name, err := readMarkdown(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(md) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no markdown given"})
		return
	}
	opts := Options{Narrator: r.FormValue("narrator"), Title: r.FormValue("title")}
	out, report := Convert(md, opts)
	if name == "" {
		name = "book.md"
	}
	filename := outputName(filepath.Base(name))
	if r.URL.Query().Get("format") == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		io.WriteString(w, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"markdown": out, "report": report, "filename": filename})
}

// send forwards converted markdown to the TTS server's /api/book (the browser can't, because of CORS).
func (s *server) send(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST markdown to this endpoint"})
		return
	}
	if s.tts == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no TTS server configured, start with -tts http://<host>:5002"})
		return
	}
	md, name, err := readMarkdown(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if name == "" {
		name = "book.book.md"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", filepath.Base(name))
	io.WriteString(part, md)
	mw.Close()

	resp, err := s.client.Post(s.tts+"/api/book", mw.FormDataContentType(), &body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "TTS server unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unexpected response from TTS server (%s)", resp.Status)})
		return
	}
	result["tts"] = s.tts
	writeJSON(w, resp.StatusCode, result)
}
