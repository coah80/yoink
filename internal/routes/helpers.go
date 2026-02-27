package routes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	ffmpegTimeRegex  = regexp.MustCompile(`time=(\d+):(\d+):(\d+\.?\d*)`)
	ffmpegSpeedRegex = regexp.MustCompile(`speed=\s*([\d.]+)x`)
)

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func formValueOr(r *http.Request, key, fallback string) string {
	v := r.FormValue(key)
	if v == "" {
		return fallback
	}
	return v
}

func intFormValue(r *http.Request, key string, fallback int) int {
	v := r.FormValue(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func floatFormValue(r *http.Request, key string, fallback float64) float64 {
	v := r.FormValue(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func toASCIIFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r <= 0x7E {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
