package util

import "testing"

func TestNormalizeYouTubeURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			"https://youtube.com/shorts/LLR5A-Y7c0s?is=wueQLx-6RwmPqdLp",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
		},
		{
			"https://www.youtube.com/shorts/LLR5A-Y7c0s",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
		},
		{
			"https://youtube.com/shorts/LLR5A-Y7c0s?si=wueQLx-6RwmPqdLp",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
		},
		{
			"https://youtu.be/LLR5A-Y7c0s",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
		},
		{
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
		},
		{
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s&t=12s",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s",
		},
		{
			"https://www.youtube.com/clip/UgkxSomething",
			"https://www.youtube.com/clip/UgkxSomething",
		},
		{
			"https://www.youtube.com/playlist?list=PLtest",
			"https://www.youtube.com/playlist?list=PLtest",
		},
		{
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s&list=PLtest",
			"https://www.youtube.com/watch?v=LLR5A-Y7c0s&list=PLtest",
		},
		{
			"https://tiktok.com/@user/video/123",
			"https://tiktok.com/@user/video/123",
		},
	}

	for _, c := range cases {
		got := NormalizeYouTubeURL(c.in)
		if got != c.want {
			t.Fatalf("NormalizeYouTubeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
