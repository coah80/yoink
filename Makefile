VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-s -w -X github.com/coah80/yoink/internal/config.Version=$(VERSION)"

.PHONY: build run clean linux windows

build:
	go build $(LDFLAGS) -o yoink ./cmd/yoink

run:
	go run $(LDFLAGS) ./cmd/yoink

linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o yoink-linux ./cmd/yoink

windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o yoink.exe ./cmd/yoink

clean:
	rm -f yoink yoink-linux yoink.exe
