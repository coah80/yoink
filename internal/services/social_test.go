package services

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type socialRoundTripper func(*http.Request) (*http.Response, error)

func (fn socialRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func socialResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestResolveTruthSocialMedia(t *testing.T) {
	client := &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://truthsocial.com/api/v1/statuses/110000000000000002" {
			t.Fatalf("request URL = %q", request.URL.String())
		}
		if request.Header.Get("User-Agent") == "" {
			t.Fatal("request has no User-Agent")
		}
		return socialResponse(`{
			"id":"110000000000000002",
			"content":"<p>James Carville talks about a schism.</p>",
			"account":{"display_name":"Citizen Free Press","username":"CitizenFreePress"},
			"media_attachments":[{
				"type":"video",
				"url":"https://static-assets-1.truthsocial.com/media/example.mp4",
				"preview_url":"https://static-assets-1.truthsocial.com/media/example.jpg",
				"meta":{"original":{"duration":167.4}}
			}]
		}`), nil
	})}

	media, handled, err := resolveSocialMedia(context.Background(), "https://truthsocial.com/@CitizenFreePress/posts/110000000000000002", 0, client)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("Truth Social URL was not handled")
	}
	if media.DirectURL != "https://static-assets-1.truthsocial.com/media/example.mp4" {
		t.Fatalf("DirectURL = %q", media.DirectURL)
	}
	if media.Title != "James Carville talks about a schism." {
		t.Fatalf("Title = %q", media.Title)
	}
	if media.Uploader != "Citizen Free Press" || media.Ext != "mp4" || media.Duration != 167.4 {
		t.Fatalf("media = %#v", media)
	}
}

func TestResolveTruthSocialMediaUsesWgetAfterGoTransportIsBlocked(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "wget")
	script := `#!/bin/sh
printf '%s' '{"id":"110000000000000002","content":"<p>Fallback video</p>","account":{"display_name":"Citizen Free Press"},"media_attachments":[{"type":"video","url":"https://static-assets-1.truthsocial.com/media/fallback.mp4"}]}'
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WGET_BIN", bin)
	client := &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		response := socialResponse("blocked")
		response.StatusCode = http.StatusForbidden
		return response, nil
	})}

	media, handled, err := resolveSocialMedia(context.Background(), "https://truthsocial.com/@someone/110000000000000002", 0, client)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || media.DirectURL != "https://static-assets-1.truthsocial.com/media/fallback.mp4" {
		t.Fatalf("media = %#v, handled = %v", media, handled)
	}
}

func TestResolveRumbleMediaSelectsRequestedHeight(t *testing.T) {
	client := &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "rumble.com":
			if request.URL.Path == "/api/Media/oembed.json" {
				return socialResponse(`{"title":"Seahorse Searches for Food","author_name":"ViralHog","duration":80,"thumbnail_url":"https://cdn.example/thumb.jpg","html":"<iframe src=\"https://rumble.com/embed/vj3lae/\"></iframe>"}`), nil
			}
			if request.URL.Path == "/embed/vj3lae/" {
				return socialResponse(`<script>m.f["vj3lae"]={"ua":{"mp4":{"360":{"url":"https:\/\/cdn.example\/360.mp4","meta":{"h":360}},"720":{"url":"https:\/\/cdn.example\/720.mp4","meta":{"h":720}},"1080":{"url":"https:\/\/cdn.example\/1080.mp4","meta":{"h":1080}}}},"title":"Seahorse Searches for Food","loaded":h()}</script>`), nil
			}
		}
		t.Fatalf("unexpected request URL = %q", request.URL.String())
		return nil, nil
	})}

	media, handled, err := resolveSocialMedia(context.Background(), "https://rumble.com/v000001-test-video.html", 720, client)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("Rumble URL was not handled")
	}
	if media.DirectURL != "https://cdn.example/720.mp4" {
		t.Fatalf("DirectURL = %q", media.DirectURL)
	}
	if media.Title != "Seahorse Searches for Food" || media.Uploader != "ViralHog" || media.Duration != 80 {
		t.Fatalf("media = %#v", media)
	}
}

func TestResolveBilibiliMediaUsesPublicAPI(t *testing.T) {
	client := &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/x/web-interface/view" {
			if request.Header.Get("Referer") != "https://www.bilibili.com/" {
				t.Fatalf("Referer = %q", request.Header.Get("Referer"))
			}
			return socialResponse(`{"code":0,"data":{"bvid":"BV1xx411c7mD","cid":41324905972,"title":"Public Bilibili video","pic":"http://i0.hdslb.com/example.jpg","duration":51,"owner":{"name":"乔乔和夫夫"}}}`), nil
		}
		if request.URL.Path == "/x/player/playurl" {
			if request.URL.Query().Get("qn") != "16" {
				t.Fatalf("qn = %q", request.URL.Query().Get("qn"))
			}
			return socialResponse(`{"code":0,"data":{"quality":16,"durl":[{"url":"https://upos.example/video.mp4","size":9317548,"length":51200}]}}`), nil
		}
		t.Fatalf("unexpected request URL = %q", request.URL.String())
		return nil, nil
	})}

	media, handled, err := resolveSocialMedia(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD", 360, client)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("Bilibili URL was not handled")
	}
	if media.DirectURL != "https://upos.example/video.mp4" || media.Title != "Public Bilibili video" {
		t.Fatalf("media = %#v", media)
	}
	if media.Thumbnail != "https://i0.hdslb.com/example.jpg" || media.Duration != 51 || media.Uploader != "乔乔和夫夫" {
		t.Fatalf("media = %#v", media)
	}
}

func TestResolvePornobaeMediaUnpacksEmbeddedHLS(t *testing.T) {
	client := &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "pornobae.com":
			return socialResponse(`<meta content="Public post" property="og:title"><meta property="og:image" content="https://pornobae.com/thumb.jpg"><iframe src="https://tubexplayer.com/embed-test123.html"></iframe>`), nil
		case "tubexplayer.com":
			if request.Header.Get("Referer") != "https://pornobae.com/example/" {
				t.Fatalf("Referer = %q", request.Header.Get("Referer"))
			}
			return socialResponse(`<script>eval(function(p,a,c,k,e,d){while(c--)if(k[c])p=p.replace(new RegExp('\\b'+c.toString(a)+'\\b','g'),k[c]);return p}('0("1").2({3:[{4:"5"}]});',36,6,'jwplayer|vplayer|setup|sources|file|https://s3.hls2.all4tube.com/path/master.m3u8?t=alpha&s=beta'.split('|'),0,{}))</script>`), nil
		default:
			t.Fatalf("unexpected request URL = %q", request.URL.String())
			return nil, nil
		}
	})}

	media, handled, err := resolveSocialMedia(context.Background(), "https://pornobae.com/example/", 720, client)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("Pornobae URL was not handled")
	}
	if media.DirectURL != "https://s3.hls2.all4tube.com/path/master.m3u8?t=alpha&s=beta" {
		t.Fatalf("DirectURL = %q", media.DirectURL)
	}
	if media.Title != "Public post" || media.Thumbnail != "https://pornobae.com/thumb.jpg" || media.ID != "test123" || media.Ext != "mp4" {
		t.Fatalf("media = %#v", media)
	}
}

func TestResolvePornobaeRejectsUnexpectedMediaHost(t *testing.T) {
	client := &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "pornobae.com" {
			return socialResponse(`<iframe src="https://tubexplayer.com/embed-test123.html"></iframe>`), nil
		}
		return socialResponse(`<script>eval(function(p,a,c,k,e,d){while(c--)if(k[c])p=p.replace(new RegExp('\\b'+c.toString(a)+'\\b','g'),k[c]);return p}('0:"1"',36,2,'file|https://example.com/master.m3u8'.split('|'),0,{}))</script>`), nil
	})}

	_, handled, err := resolveSocialMedia(context.Background(), "https://pornobae.com/example/", 0, client)
	if !handled || err == nil || !strings.Contains(err.Error(), "invalid Pornobae media URL") {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
}

func TestResolveSocialMediaIgnoresOtherSites(t *testing.T) {
	media, handled, err := resolveSocialMedia(context.Background(), "https://example.com/video", 0, http.DefaultClient)
	if err != nil || handled || media != nil {
		t.Fatalf("media = %#v, handled = %v, err = %v", media, handled, err)
	}
}
