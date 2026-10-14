package util

import "testing"

func TestToUserErrorSiteRequirements(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{
			message: "ERROR: [Douyin] Fresh cookies (not necessarily logged in) are needed",
			want:    "This site requires fresh browser cookies",
		},
		{
			message: "WARNING: connection closed ERROR: No video could be found in this post",
			want:    "No downloadable video found in this post",
		},
		{
			message: "ERROR: HTTP Error 429: Too Many Requests",
			want:    "This site is temporarily rate limiting downloads",
		},
		{
			message: "ERROR: This format is DRM protected",
			want:    "This video is DRM protected and cannot be downloaded",
		},
	}

	for _, test := range tests {
		if got := ToUserError(test.message); got != test.want {
			t.Fatalf("ToUserError(%q) = %q, want %q", test.message, got, test.want)
		}
	}
}
