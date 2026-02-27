package util

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/coah80/yoink/internal/config"
)

func HasProxy() bool {
	return config.ProxyHost != "" && config.ProxyUserPrefix != "" && config.ProxyPassword != "" && config.ProxyCount > 0
}

func GetRandomProxyURL() string {
	if !HasProxy() {
		return ""
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(config.ProxyCount)))
	if err != nil {
		nBig = big.NewInt(1)
	}
	n := nBig.Int64() + 1
	return fmt.Sprintf("http://%s-%d:%s@%s:%s",
		config.ProxyUserPrefix, n, config.ProxyPassword,
		config.ProxyHost, config.ProxyPort)
}

func GetProxyArgs() []string {
	url := GetRandomProxyURL()
	if url == "" {
		return nil
	}
	return []string{"--proxy", url}
}
