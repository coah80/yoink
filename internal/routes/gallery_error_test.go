package routes

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coah80/yoink/internal/services"
)

func TestGalleryErrorDoesNotExposeUpstreamHTML(t *testing.T) {
	for _, extra := range []string{"", "playlist too large", "too many active jobs"} {
		t.Run(extra, func(t *testing.T) {
			w := httptest.NewRecorder()
			p := &services.ProcessInfo{}
			p.SetCancelled(true)
			cleaned := false
			galleryError(w, "audit-gallery-error", p, fmt.Errorf("gallery-dl failed: [reddit][error] <html><style>.theme-light,:root{--rem360:22.5rem}</style>blocked %s</html>", extra), func() { cleaned = true })
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result["error"], "<") || strings.Contains(result["error"], "theme-light") || result["error"] == "" {
				t.Fatalf("unsafe response: %s", w.Body)
			}
			if !cleaned {
				t.Fatal("cleanup not called")
			}
		})
	}
}
