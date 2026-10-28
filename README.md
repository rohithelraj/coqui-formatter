# 🐸 coqui-formatter

A tiny web server that converts **any markdown** (stories, book chapters, articles) into
**Book Markdown**, the input format of the
[coqui TTS audiobook server](https://github.com/rohithelraj/TTS-for-pi-5/blob/dev/TTS/server/BOOK_FORMAT.md).

- **Lightweight:** a single ~6 MB static Go binary with no dependencies and ~16 MB of RAM. Runs on a
  **Raspberry Pi Zero**.
- **Browser UI:** drop or paste a file, review the result and the guessed lines, edit it, then download it
  or send it straight to the TTS server.
- **Offline and rule-based:** no AI model, no internet, deterministic output.

```
 any .md ──▶ coqui-formatter (Pi Zero, :8080) ──▶ .book.md ──▶ coqui TTS (Pi 5, :5002) ──▶ .mp3 / .m4b
```

## What it does

| Input | Output |
|---|---|
| `"Hello," said Alice.` / `Alice said, "Hello."` / `"Hello," Alice said quietly.` | `> **Alice:** Hello.` |
| `"Wait," Tom said. "Don't go."` | `> **Tom:** Wait. Don't go.` |
| `"Hi," said the old man.` / `"No," her father replied.` | speakers `Old Man`, `Father` |
| `Alice frowned. "Why?" she asked.` | `she` resolved to Alice |
| untagged back-and-forth lines | speakers alternate (reported as guesses) |
| `‘I shan’t,’ said Margaret.` (British quotes) | detected automatically |
| `"Run," I said.` (first person) | read by the narrator |
| `the so-called "expert"` | left alone (not dialogue) |
| `* * *`, `***`, `#`, `⁂` | `---` scene break |
| `Chapter IV` | `Chapter Four` |
| one `#` title with `##` chapters | headings promoted, so each chapter gets an M4B chapter marker |
| `&`, `50%`, `e.g.`, `--` | "and", "50 percent", "for example", "—" |
| images, tables, code blocks, footnotes, HTML, comments | removed |
| existing front matter | `title`, `author`, `pauses` and already-chosen voices are kept |

It also writes the `voices:` front matter. The narrator defaults to `p230`, and each character gets a
distinct VCTK voice matched to a detected gender (from Mr/Mrs, father/mother, he/his/she/her…).
Converting an already converted file gives the same output again.

**Rules can't understand a story the way a person can.** When a speaker isn't stated, the converter
guesses from context. If it can't, it leaves the line as narration rather than risk the wrong voice.
Every guessed or unattributed line appears in the report with its line number, so check those before
rendering. For hard cases (long multi-person scenes with few speech tags), the `book-formatter`
Claude Code agent in the TTS repo does better.

## Install on a Raspberry Pi Zero

Prebuilt binaries are made with `make pi`:

| Board | OS | Binary |
|---|---|---|
| Pi Zero, Zero W, Pi 1 | 32-bit Raspberry Pi OS | `dist/coqui-formatter-linux-armv6` |
| Pi Zero 2 W, Pi 3/4/5 | 64-bit Raspberry Pi OS | `dist/coqui-formatter-linux-arm64` |

On a Zero 2 W running a 32-bit OS, the armv6 binary also works. Check with `uname -m`: `armv6l`/`armv7l`
→ armv6, `aarch64` → arm64.

1. Flash **Raspberry Pi OS Lite** with Raspberry Pi Imager. Set the hostname (e.g. `formatter-pi`), user,
   Wi-Fi and SSH in the imager settings.
2. Build on your computer and copy the binary over (Go 1.21+ needed only for building):
   ```bash
   make pi
   scp dist/coqui-formatter-linux-armv6 <user>@formatter-pi.local:/tmp/coqui-formatter
   ```
3. On the Pi:
   ```bash
   sudo install -m 755 /tmp/coqui-formatter /usr/local/bin/coqui-formatter
   coqui-formatter -tts http://tts-pi.local:5002
   ```
4. Open **`http://formatter-pi.local:8080`** in a browser on any device on your network.

`-tts` is optional. It enables the **Send to TTS server** button, which queues the book on your Pi 5
running coqui TTS. Use the hostname or IP of that Pi.

### Run on boot

```bash
sudo cp coqui-formatter.service /etc/systemd/system/
sudo nano /etc/systemd/system/coqui-formatter.service   # set User= and the -tts URL
sudo systemctl daemon-reload
sudo systemctl enable --now coqui-formatter
journalctl -u coqui-formatter -f
```

It can also run on the Pi 5 next to the TTS server. Use the arm64 binary and `-tts http://localhost:5002`.

## Use

### Browser

Drop or paste markdown, pick the narrator voice, then click **Convert**. The right side shows the Book
Markdown (editable), the character → voice table, warnings, and a list of guessed lines to check.
Then **Download** `name.book.md`, **Copy** it, or **Send to TTS server**.

### Command line

The same binary converts files without the server:

```bash
coqui-formatter convert chapter1.md                  # writes chapter1.book.md, report on stderr
coqui-formatter convert -narrator p228 -o - ch1.md   # to stdout
```

### HTTP API

```bash
# JSON: {"markdown": "...", "filename": "ch1.book.md", "report": {...}}
curl -F file=@ch1.md http://formatter-pi.local:8080/api/convert

# just the converted markdown
curl -F file=@ch1.md "http://formatter-pi.local:8080/api/convert?format=md" -o ch1.book.md

# raw body works too; options as form fields or query: narrator, title, name
curl --data-binary @ch1.md "http://formatter-pi.local:8080/api/convert?name=ch1.md&format=md"

# forward converted markdown to the TTS server configured with -tts
curl -F file=@ch1.book.md http://formatter-pi.local:8080/api/send
```

| Endpoint | |
|---|---|
| `GET /` | web UI |
| `POST /api/convert` | convert (`file`, `text`, or raw body; `narrator`, `title`, `name`; `?format=md`) |
| `POST /api/send` | forward to `<tts>/api/book` |
| `GET /api/config` | narrator default, voice lists, TTS URL |
| `GET /healthz` | `ok` |

## Options

```
coqui-formatter [serve] [-addr :8080] [-tts http://tts-pi.local:5002]
coqui-formatter convert [-narrator p230] [-title T] [-o out.book.md] file.md
```

## Development

```bash
make test     # unit + HTTP handler tests
make run      # http://localhost:8080
make pi       # cross-compile for the Pis into dist/
```

The code is small: `convert.go` (conversion rules), `voices.go` (VCTK voice pools, kept in sync with the
voice table in the TTS repo's `BOOK_FORMAT.md`), `main.go` (server and CLI), `web/index.html`
(embedded UI).

There is no authentication. Keep it on your home network.
