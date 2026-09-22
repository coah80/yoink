package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"strings"

	"github.com/coah80/yoink/internal/config"
)

func HasProxy() bool {
	return len(proxyHosts()) > 0
}

func GetRandomProxyURL() string {
	hosts := proxyHosts()
	if len(hosts) == 0 {
		return ""
	}

	host := hosts[0]
	if len(hosts) > 1 {
		host = hosts[randIndex(len(hosts))]
	}

	if strings.Contains(host, "://") {
		return host
	}

	scheme := proxyScheme()
	user, pass := proxyUserPass()
	if user != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%s", scheme, url.PathEscape(user), url.PathEscape(pass), host, proxyPort())
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, proxyPort())
}

func GetProxyArgs() []string {
	u := GetRandomProxyURL()
	if u == "" {
		return nil
	}
	return []string{"--proxy", u}
}

func proxyHosts() []string {
	var hosts []string
	for _, h := range strings.Split(config.ProxyHost, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

func proxyScheme() string {
	if config.ProxyScheme != "" {
		return config.ProxyScheme
	}
	if config.ProxyPassword != "" {
		return "http"
	}
	return "socks5h"
}

func proxyPort() string {
	if config.ProxyPort != "" {
		return config.ProxyPort
	}
	if config.ProxyPassword != "" {
		return "80"
	}
	return "1080"
}

func proxyUserPass() (string, string) {
	if config.ProxyUserPrefix == "" || config.ProxyPassword == "" {
		return "", ""
	}
	user := config.ProxyUserPrefix
	if config.ProxyCount > 1 {
		user = fmt.Sprintf("%s-%d", config.ProxyUserPrefix, randIndex(config.ProxyCount)+1)
	}
	return user, config.ProxyPassword
}

func randIndex(n int) int {
	if n <= 1 {
		return 0
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(nBig.Int64())
}
