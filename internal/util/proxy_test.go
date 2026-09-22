package util

import (
	"strings"
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
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		got := GetRandomProxyURL()
		if !strings.HasPrefix(got, "socks5h://") || !strings.HasSuffix(got, ":1080") {
			t.Fatalf("bad url %q", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
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
	got := GetRandomProxyURL()
	if !strings.HasPrefix(got, "http://yoink-") || !strings.Contains(got, ":secret@proxy.example:80") {
		t.Fatalf("got %q", got)
	}
}

func TestGetProxyArgs_Empty(t *testing.T) {
	resetProxyConfig()
	if GetProxyArgs() != nil {
		t.Fatal("no host should yield no args")
	}
}
