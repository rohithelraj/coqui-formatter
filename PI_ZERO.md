# Setting up coqui-formatter on a Raspberry Pi Zero

Step-by-step guide for after you have cloned this repo on the Pi. At the end, the converter runs as
a service on boot and is reachable from any browser on your network at
`http://<pi-hostname>.local:8080`.

This guide assumes **Raspberry Pi OS Lite** with only SSH: no desktop, no monitor. You type every
command in an SSH session from your computer. The web page is opened in the browser on your
computer or phone, never on the Pi itself.

Optional: the coqui TTS server already running on your Raspberry Pi 5, for the "Send to TTS server"
button. See the
[TTS-for-pi-5 guide](https://github.com/rohithelraj/TTS-for-pi-5/blob/dev/RASPBERRY_PI_5.md).

---

## Step 0: Connect, update, and get the code

From a terminal on your computer (macOS/Linux Terminal, or PowerShell on Windows):

```bash
ssh <user>@<pi-hostname>.local      # e.g. ssh rohith@formatter-pi.local
```

Use the username and hostname you set in Raspberry Pi Imager. If `.local` doesn't resolve, use the
Pi's IP address from your router's device list.

Everything from here on runs **on the Pi Zero, inside this SSH session**.

```bash
sudo apt update && sudo apt full-upgrade -y     # can take a while on a Zero
sudo apt install -y git curl
git clone https://github.com/rohithelraj/coqui-formatter.git ~/coqui-formatter
cd ~/coqui-formatter
```

If `apt full-upgrade` installed a new kernel, `sudo reboot`, wait a minute, and `ssh` in again.

---

## Step 1: Check which Pi / OS you have

```bash
uname -m
```

| Output | Board / OS | Go download to use in step 2 |
|---|---|---|
| `armv6l` | Pi Zero, Zero W, Pi 1 | `armv6l` |
| `armv7l` | Pi Zero 2 W (or Pi 2/3/4) with a 32-bit OS | `armv6l` |
| `aarch64` | Pi Zero 2 W (or Pi 3/4/5) with a 64-bit OS | `arm64` |

## Step 2: Install Go

Go is only needed to build the program, about once per update. The version in `apt` is often too old,
so install the official build from go.dev:

```bash
# pick the right download for your board (see the table above)
case "$(uname -m)" in
  aarch64) GOARCH_DL=arm64 ;;
  *)       GOARCH_DL=armv6l ;;
esac
GO_VERSION=$(curl -s "https://go.dev/VERSION?m=text" | head -1)
echo "Installing $GO_VERSION for $GOARCH_DL"

curl -LO "https://go.dev/dl/${GO_VERSION}.linux-${GOARCH_DL}.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "${GO_VERSION}.linux-${GOARCH_DL}.tar.gz"
rm "${GO_VERSION}.linux-${GOARCH_DL}.tar.gz"

# put Go on the PATH, now and for future logins
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
export PATH=$PATH:/usr/local/go/bin

go version   # should print e.g. "go version go1.27.3 linux/arm"
```

This needs about 300 MB of free space on the SD card. Check with `df -h /`.

## Step 3: Build

```bash
cd ~/coqui-formatter
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o coqui-formatter .
ls -lh coqui-formatter    # ~6 MB
```

- On an original Pi Zero (single core) the **first build can take several minutes** while Go compiles
  its standard library. Later builds are much faster. No output means success.
- If the build is `Killed` (out of memory), see [Troubleshooting](#troubleshooting).
- Optional: run the tests with `go test ./...`. Expect `ok`.

## Step 4: Try it

```bash
./coqui-formatter -tts http://tts-pi.local:5002
```

- Replace `tts-pi.local` with the hostname or IP of your Pi 5 running coqui TTS. Leave out
  `-tts …` entirely if you don't use it. The **Send to TTS server** button is then hidden.
- It prints `coqui-formatter … listening on :8080`.
- On your phone or computer, open **`http://<pi-hostname>.local:8080`**. Find the hostname with
  `hostname` or the IP with `hostname -I` on the Pi.
- Paste some text with dialogue, click **Convert**, and check the result.

Press `Ctrl+C` to stop it. Started this way, it also stops when you close the SSH session. Step 6
makes it run in the background permanently.

## Step 5: Install the program

```bash
sudo install -m 755 coqui-formatter /usr/local/bin/coqui-formatter
coqui-formatter -h    # prints the usage
```

## Step 6: Start it automatically on boot

The repo includes a systemd unit. Install it with your username and TTS server URL filled in.
Change `TTS_URL`, or set it to empty (`TTS_URL=`) if you don't have a TTS server:

```bash
TTS_URL=http://tts-pi.local:5002

sed -e "s|^User=.*|User=$USER|" \
    -e "s|-tts [^ ]*|${TTS_URL:+-tts $TTS_URL}|" \
    coqui-formatter.service | sudo tee /etc/systemd/system/coqui-formatter.service

sudo systemctl daemon-reload
sudo systemctl enable --now coqui-formatter
```

Check it:

```bash
systemctl status coqui-formatter        # "active (running)"
curl http://localhost:8080/healthz      # "ok"
journalctl -u coqui-formatter -f        # live log, Ctrl+C to leave
```

Reboot once (`sudo reboot`; your SSH session closes), wait a minute, and open the page in your
browser again, without logging in. That proves it starts on boot on its own. You can now close SSH.
The Pi only needs power and Wi-Fi.

To change the port or TTS URL later, edit `/etc/systemd/system/coqui-formatter.service`, then run
`sudo systemctl daemon-reload && sudo systemctl restart coqui-formatter`.

## Step 7: Use it

- **Browser:** `http://<pi-hostname>.local:8080`. Drop a `.md` file, review the character voices
  and the **guessed lines** list, edit if needed, then **Download** or **Send to TTS server**.
- **Command line on the Pi:** `coqui-formatter convert chapter1.md` writes `chapter1.book.md`.
- **From other machines:**
  `curl -F file=@ch1.md "http://<pi-hostname>.local:8080/api/convert?format=md" -o ch1.book.md`

More options and the full API are in the [README](README.md).

---

## Updating

```bash
cd ~/coqui-formatter
git pull
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o coqui-formatter .
sudo install -m 755 coqui-formatter /usr/local/bin/coqui-formatter
sudo systemctl restart coqui-formatter
```

## Freeing space (optional)

The service only needs the binary. If the SD card is small, you can remove Go and the build cache.
Reinstall Go (step 2) before your next update.

```bash
go clean -cache -modcache
sudo rm -rf /usr/local/go
```

## Uninstalling

```bash
sudo systemctl disable --now coqui-formatter
sudo rm /etc/systemd/system/coqui-formatter.service /usr/local/bin/coqui-formatter
sudo systemctl daemon-reload
```

## Troubleshooting

| Problem | Fix |
|---|---|
| `go: command not found` | Run `export PATH=$PATH:/usr/local/go/bin`, or log out and back in |
| `cannot execute binary file: Exec format error` (when running `go`) | Wrong Go download for your board. Redo step 2; check `uname -m`. |
| Build fails with `Killed` | Out of memory. Add temporary swap and retry: `sudo fallocate -l 1G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile`. Building with `go build -p 1 …` also uses less memory. |
| `go.mod requires go >= …` | Your Go is too old. Install the current one with step 2. |
| `address already in use` | Something else uses port 8080. Start with `-addr :8081` (in the service file too). |
| Page doesn't open from other devices | Use the IP instead of `.local` (`hostname -I`). Check `systemctl status coqui-formatter`. Make sure both devices are on the same network. |
| "TTS server unreachable" on **Send** | Check the URL from the Pi Zero: `curl http://tts-pi.local:5002/book`. Is the Pi 5 service running? |
| Send fails with "Unknown speaker ids" | The TTS server isn't running a multi-speaker model. Start it with `--model_name tts_models/en/vctk/vits`. |
| Wrong voice for a line | Check the "guessed lines" list in the UI. Edit the `> **Name:**` line, or the voice in the front matter, before sending. |

There is no login. Keep it on your home network and don't forward port 8080 on your router.
