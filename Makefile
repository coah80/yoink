VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-s -w -X github.com/coah80/yoink/internal/config.Version=$(VERSION)"

.PHONY: build bot run clean linux linux-bot windows

build:
	go build $(LDFLAGS) -o yoink ./cmd/yoink

bot:
	go build $(LDFLAGS) -o yoink-bot ./cmd/bot

run:
	go run $(LDFLAGS) ./cmd/yoink

linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o yoink-linux ./cmd/yoink

linux-bot:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o yoink-bot-linux ./cmd/bot

windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o yoink.exe ./cmd/yoink

clean:
	rm -f yoink yoink-bot yoink-linux yoink-bot-linux yoink.exe
