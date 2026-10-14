package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseYouTubeClipIgnoresRecommendedVideo(t *testing.T) {
	page := `<script>var ytInitialData = {"recommendation":{"videoId":"TpFj3Q8vrfQ"},"currentVideoEndpoint":{"watchEndpoint":{"videoId":"IwELiBZpQw4"}}}; var ytInitialPlayerResponse = {"clipConfig":{"startTimeMs":"1459923","endTimeMs":"1474923"}};</script>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }))
	defer server.Close()
	clip, err := ParseYouTubeClip(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if clip.VideoID != "IwELiBZpQw4" || clip.StartTimeMs != 1459923 || clip.EndTimeMs != 1474923 {
		t.Fatalf("wrong source or interval: %+v", clip)
	}
}

func TestParseYouTubeClipRejectsMissingParentAndInvalidRange(t *testing.T) {
	for _, page := range []string{
		`<script>var ytInitialData={"recommendation":{"videoId":"TpFj3Q8vrfQ"}};var ytInitialPlayerResponse={"clipConfig":{"startTimeMs":"1","endTimeMs":"2"}};</script>`,
		`<script>var ytInitialData={"currentVideoEndpoint":{"watchEndpoint":{"videoId":"IwELiBZpQw4"}}};var ytInitialPlayerResponse={"clipConfig":{"startTimeMs":"20","endTimeMs":"10"}};</script>`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }))
		_, err := ParseYouTubeClip(context.Background(), server.URL)
		server.Close()
		if err == nil {
			t.Fatal("accepted missing parent or invalid range")
		}
	}
}

func TestParseYouTubeClipSupportsPlayerDetailsAndZeroStart(t *testing.T) {
	page := `<script>var ytInitialPlayerResponse={"videoDetails":{"videoId":"IwELiBZpQw4"},"clipConfig":{"startTimeMs":0,"endTimeMs":15000}};</script>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }))
	defer server.Close()
	clip, err := ParseYouTubeClip(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if clip.VideoID != "IwELiBZpQw4" || clip.StartTimeMs != 0 || clip.EndTimeMs != 15000 {
		t.Fatalf("wrong clip: %+v", clip)
	}
}
