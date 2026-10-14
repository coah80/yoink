package util

import (
	"regexp"
	"testing"

	"github.com/coah80/yoink/internal/config"
)

func resetProxyConfig() {
	config.ProxyHost = ""
	config.ProxyPort = ""
	config.ProxyScheme = ""
	config.ProxyUserPrefix = ""
	config.ProxyPassword = ""
	config.ProxyCount = 0
}

func TestHasProxy(t *testing.T) {
	resetProxyConfig()
	if HasProxy() {
		t.Fatal("empty host should not count as a proxy")
	}
	config.ProxyHost = "10.64.0.1"
	if !HasProxy() {
		t.Fatal("mullvad socks host should be enough")
	}
}

func TestGetRandomProxyURL_Mullvad(t *testing.T) {
	resetProxyConfig()
	config.ProxyHost = "10.64.0.1"
	config.ProxyPort = "1080"
	got := GetRandomProxyURL()
	want := "socks5h://10.64.0.1:1080"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGetRandomProxyURL_MullvadRelays(t *testing.T) {
	resetProxyConfig()
	config.ProxyHost = "se-mma-wg-socks5-001.relays.mullvad.net, nl-ams-wg-socks5-001.relays.mullvad.net"
	want := map[string]bool{
		"socks5h://se-mma-wg-socks5-001.relays.mullvad.net:1080": true,
		"socks5h://nl-ams-wg-socks5-001.relays.mullvad.net:1080": true,
	}
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		got := GetRandomProxyURL()
		if !want[got] {
			t.Fatalf("unexpected relay url %q", got)
		}
		seen[got] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("expected both relays, got %v", seen)
	}
}

func TestGetRandomProxyURL_AuthPool(t *testing.T) {
	resetProxyConfig()
	config.ProxyHost = "proxy.example"
	config.ProxyPort = "80"
	config.ProxyUserPrefix = "yoink"
	config.ProxyPassword = "secret"
	config.ProxyCount = 3
	re := regexp.MustCompile(`^http://yoink-[1-3]:secret@proxy\.example:80$`)
	for i := 0; i < 40; i++ {
		got := GetRandomProxyURL()
		if !re.MatchString(got) {
			t.Fatalf("got %q", got)
		}
	}
}

func TestGetProxyArgs_Empty(t *testing.T) {
	resetProxyConfig()
	if GetProxyArgs() != nil {
		t.Fatal("no host should yield no args")
	}
}

func TestGetRandomProxyURL_SinglePoolUser(t *testing.T) {
	resetProxyConfig()
	t.Cleanup(resetProxyConfig)
	config.ProxyHost = "proxy.example"
	config.ProxyUserPrefix = "yoink"
	config.ProxyPassword = "secret"
	config.ProxyCount = 1
	if got, want := GetRandomProxyURL(), "http://yoink-1:secret@proxy.example:80"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGetRandomProxyURL_EscapesCredentials(t *testing.T) {
	resetProxyConfig()
	t.Cleanup(resetProxyConfig)
	config.ProxyHost = "proxy.example"
	config.ProxyUserPrefix = "yoink"
	config.ProxyPassword = "p@ss:word"
	if got, want := GetRandomProxyURL(), "http://yoink:p%40ss%3Aword@proxy.example:80"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestShouldRetryWithNewProxy(t *testing.T) {
	for msg, want := range map[string]bool{
		"Sign in to confirm you're not a bot":                            true,
		"Video unavailable. This video is private":                       true,
		"Video unavailable. This video has been removed by the uploader": false,
		"Download cancelled":                                             false,
		"Unsupported URL: https://example.com":                           false,
	} {
		if got := ShouldRetryWithNewProxy(msg); got != want {
			t.Errorf("ShouldRetryWithNewProxy(%q) = %v, want %v", msg, got, want)
		}
	}
}
