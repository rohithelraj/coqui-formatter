VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build run test pi-zero pi-zero2 pi all clean

build:            ## binary for this machine
	$(BUILD) -o coqui-formatter .

run: build        ## run locally on :8080
	./coqui-formatter

test:
	go test ./...

pi-zero:          ## Raspberry Pi Zero / Zero W / Pi 1 (ARMv6, 32-bit OS)
	GOOS=linux GOARCH=arm GOARM=6 $(BUILD) -o dist/coqui-formatter-linux-armv6 .

pi-zero2:         ## Pi Zero 2 W, Pi 3/4/5 with 64-bit OS
	GOOS=linux GOARCH=arm64 $(BUILD) -o dist/coqui-formatter-linux-arm64 .

pi: pi-zero pi-zero2

all: test build pi

clean:
	rm -rf coqui-formatter dist
