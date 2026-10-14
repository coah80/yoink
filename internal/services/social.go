package services

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const socialUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36"

var socialHTMLTagRe = regexp.MustCompile(`<[^>]+>`)
var rumbleIframeRe = regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)`)
var socialMetaTagRe = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
var socialHTMLAttributeRe = regexp.MustCompile(`(?i)([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
var pornobaeIframeRe = regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']*tubexplayer\.com/embed-[^"']+)`)
var packedScriptRe = regexp.MustCompile(`(?s)eval\(function\(p,a,c,k,e,d\).*?\}\('((?:\\.|[^'\\])*)',(\d+),(\d+),'((?:\\.|[^'\\])*)'\.split\('\|'\)`)
var packedMediaURLRe = regexp.MustCompile(`(?i)file\s*:\s*["'](https?://[^"']+\.m3u8[^"']*)`)

type ResolvedMedia struct {
	DirectURL string
	Title     string
	Ext       string
	ID        string
	Uploader  string
	Duration  float64
	Thumbnail string
	UserAgent string
	Referer   string
	SkipSleep bool
}

type truthStatus struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Account struct {
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
	} `json:"account"`
	MediaAttachments []struct {
		Type       string `json:"type"`
		URL        string `json:"url"`
		PreviewURL string `json:"preview_url"`
		Meta       struct {
			Original struct {
				Duration float64 `json:"duration"`
			} `json:"original"`
		} `json:"meta"`
	} `json:"media_attachments"`
}

type rumbleOEmbed struct {
	Title        string  `json:"title"`
	AuthorName   string  `json:"author_name"`
	HTML         string  `json:"html"`
	Duration     float64 `json:"duration"`
	ThumbnailURL string  `json:"thumbnail_url"`
}

type rumbleFormat struct {
	URL  string `json:"url"`
	Meta struct {
		Height int `json:"h"`
	} `json:"meta"`
}

type bilibiliViewResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BVID     string  `json:"bvid"`
		CID      int64   `json:"cid"`
		Title    string  `json:"title"`
		Picture  string  `json:"pic"`
		Duration float64 `json:"duration"`
		Owner    struct {
			Name string `json:"name"`
		} `json:"owner"`
	} `json:"data"`
}

type bilibiliPlayResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		DURL []struct {
			URL string `json:"url"`
		} `json:"durl"`
	} `json:"data"`
}

var socialHTTPClient = &http.Client{Timeout: 30 * time.Second}

func ResolveSocialMedia(ctx context.Context, rawURL string, maxHeight int) (*ResolvedMedia, bool, error) {
	return resolveSocialMedia(ctx, rawURL, maxHeight, socialHTTPClient)
}

func resolveSocialMedia(ctx context.Context, rawURL string, maxHeight int, client *http.Client) (*ResolvedMedia, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, false, nil
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case (host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com")) && strings.Contains(parsed.Path, "/video/"):
		media, err := resolveBilibili(ctx, parsed, maxHeight, client)
		return media, true, err
	case host == "truthsocial.com" || strings.HasSuffix(host, ".truthsocial.com"):
		media, err := resolveTruthSocial(ctx, parsed, client)
		return media, true, err
	case host == "rumble.com" || strings.HasSuffix(host, ".rumble.com"):
		if strings.Contains(parsed.Path, "/embed/") || strings.Contains(parsed.Path, "/api/") {
			return nil, false, nil
		}
		media, err := resolveRumble(ctx, rawURL, maxHeight, client)
		return media, true, err
	case host == "pornobae.com" || strings.HasSuffix(host, ".pornobae.com"):
		media, err := resolvePornobae(ctx, parsed, client)
		return media, true, err
	default:
		return nil, false, nil
	}
}

func resolvePornobae(ctx context.Context, parsed *url.URL, client *http.Client) (*ResolvedMedia, error) {
	body, err := fetchSocialBody(ctx, client, parsed.String(), "text/html")
	if err != nil {
		return nil, err
	}
	match := pornobaeIframeRe.FindSubmatch(body)
	if len(match) < 2 {
		return nil, fmt.Errorf("no video could be found in this Pornobae post")
	}
	embedURL, err := parsed.Parse(html.UnescapeString(string(match[1])))
	if err != nil || !isAllowedHost(embedURL, "tubexplayer.com") {
		return nil, fmt.Errorf("invalid Pornobae player URL")
	}
	embedBody, err := fetchSocialBodyWithReferer(ctx, client, embedURL.String(), "text/html", parsed.String())
	if err != nil {
		return nil, err
	}
	unpacked, err := unpackPackedScript(string(embedBody))
	if err != nil {
		return nil, fmt.Errorf("no video could be found in this Pornobae post")
	}
	mediaMatch := packedMediaURLRe.FindStringSubmatch(unpacked)
	if len(mediaMatch) < 2 {
		return nil, fmt.Errorf("no video could be found in this Pornobae post")
	}
	directURL := html.UnescapeString(strings.ReplaceAll(mediaMatch[1], `\/`, "/"))
	directParsed, err := url.Parse(directURL)
	if err != nil || !isAllowedHost(directParsed, "all4tube.com") {
		return nil, fmt.Errorf("invalid Pornobae media URL")
	}
	id := strings.TrimSuffix(path.Base(embedURL.Path), path.Ext(embedURL.Path))
	id = strings.TrimPrefix(id, "embed-")
	title := socialMetaContent(body, "og:title")
	if title == "" {
		title = "Pornobae video"
	}
	return &ResolvedMedia{
		DirectURL: directURL,
		Title:     title,
		Ext:       "mp4",
		ID:        id,
		Uploader:  "Pornobae",
		Thumbnail: socialMetaContent(body, "og:image"),
		UserAgent: socialUserAgent,
		Referer:   embedURL.String(),
		SkipSleep: true,
	}, nil
}

func isAllowedHost(parsed *url.URL, domain string) bool {
	host := strings.ToLower(parsed.Hostname())
	return parsed.Scheme == "https" && (host == domain || strings.HasSuffix(host, "."+domain))
}

func socialMetaContent(body []byte, property string) string {
	for _, tag := range socialMetaTagRe.FindAll(body, -1) {
		attributes := map[string]string{}
		for _, match := range socialHTMLAttributeRe.FindAllSubmatch(tag, -1) {
			value := ""
			for i := 2; i < len(match); i++ {
				if len(match[i]) > 0 {
					value = string(match[i])
					break
				}
			}
			attributes[strings.ToLower(string(match[1]))] = html.UnescapeString(value)
		}
		if strings.EqualFold(attributes["property"], property) || strings.EqualFold(attributes["name"], property) {
			return strings.TrimSpace(attributes["content"])
		}
	}
	return ""
}

func unpackPackedScript(script string) (string, error) {
	match := packedScriptRe.FindStringSubmatch(script)
	if len(match) < 5 {
		return "", fmt.Errorf("packed script not found")
	}
	payload, err := unescapeJavaScriptString(match[1])
	if err != nil {
		return "", err
	}
	dictionaryText, err := unescapeJavaScriptString(match[4])
	if err != nil {
		return "", err
	}
	radix, err := strconv.Atoi(match[2])
	if err != nil || radix < 2 || radix > 62 {
		return "", fmt.Errorf("invalid packed script radix")
	}
	count, err := strconv.Atoi(match[3])
	if err != nil || count < 0 || count > 10000 {
		return "", fmt.Errorf("invalid packed script dictionary size")
	}
	dictionary := strings.Split(dictionaryText, "|")
	for i := count - 1; i >= 0; i-- {
		if i >= len(dictionary) || dictionary[i] == "" {
			continue
		}
		token := packerToken(i, radix)
		tokenRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(token) + `\b`)
		word := dictionary[i]
		payload = tokenRe.ReplaceAllStringFunc(payload, func(string) string { return word })
	}
	return payload, nil
}

func packerToken(value, radix int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if value == 0 {
		return "0"
	}
	var token []byte
	for value > 0 {
		token = append(token, alphabet[value%radix])
		value /= radix
	}
	for left, right := 0, len(token)-1; left < right; left, right = left+1, right-1 {
		token[left], token[right] = token[right], token[left]
	}
	return string(token)
}

func unescapeJavaScriptString(value string) (string, error) {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		i++
		if i >= len(value) {
			return "", fmt.Errorf("invalid JavaScript escape")
		}
		switch value[i] {
		case '\\', '\'', '"', '/':
			result.WriteByte(value[i])
		case 'b':
			result.WriteByte('\b')
		case 'f':
			result.WriteByte('\f')
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 't':
			result.WriteByte('\t')
		case 'v':
			result.WriteByte('\v')
		case 'x':
			if i+2 >= len(value) {
				return "", fmt.Errorf("invalid JavaScript hex escape")
			}
			decoded, err := strconv.ParseUint(value[i+1:i+3], 16, 8)
			if err != nil {
				return "", err
			}
			result.WriteByte(byte(decoded))
			i += 2
		case 'u':
			if i+4 >= len(value) {
				return "", fmt.Errorf("invalid JavaScript Unicode escape")
			}
			decoded, err := strconv.ParseUint(value[i+1:i+5], 16, 16)
			if err != nil {
				return "", err
			}
			result.WriteRune(rune(decoded))
			i += 4
		case '\n':
		case '\r':
			if i+1 < len(value) && value[i+1] == '\n' {
				i++
			}
		default:
			result.WriteByte(value[i])
		}
	}
	return result.String(), nil
}

func resolveBilibili(ctx context.Context, parsed *url.URL, maxHeight int, client *http.Client) (*ResolvedMedia, error) {
	bvid := ""
	for _, part := range strings.Split(strings.Trim(parsed.Path, "/"), "/") {
		if strings.HasPrefix(strings.ToUpper(part), "BV") {
			bvid = part
			break
		}
	}
	if bvid == "" {
		return nil, fmt.Errorf("no video could be found in this Bilibili URL")
	}

	viewURL := "https://api.bilibili.com/x/web-interface/view?" + url.Values{"bvid": {bvid}}.Encode()
	body, err := fetchSocialBody(ctx, client, viewURL, "application/json")
	if err != nil {
		return nil, err
	}
	var view bilibiliViewResponse
	if err := json.Unmarshal(body, &view); err != nil {
		return nil, fmt.Errorf("Bilibili returned invalid metadata: %w", err)
	}
	if view.Code != 0 || view.Data.CID == 0 {
		return nil, fmt.Errorf("Bilibili metadata failed: %s", view.Message)
	}

	values := url.Values{
		"bvid":  {bvid},
		"cid":   {strconv.FormatInt(view.Data.CID, 10)},
		"qn":    {strconv.Itoa(bilibiliQuality(maxHeight))},
		"fnval": {"0"},
		"fourk": {"1"},
	}
	playURL := "https://api.bilibili.com/x/player/playurl?" + values.Encode()
	body, err = fetchSocialBody(ctx, client, playURL, "application/json")
	if err != nil {
		return nil, err
	}
	var play bilibiliPlayResponse
	if err := json.Unmarshal(body, &play); err != nil {
		return nil, fmt.Errorf("Bilibili returned invalid video metadata: %w", err)
	}
	if play.Code != 0 || len(play.Data.DURL) == 0 {
		return nil, fmt.Errorf("no video formats found for this Bilibili post")
	}
	directURL := play.Data.DURL[0].URL
	if err := validateDirectMediaURL(directURL); err != nil {
		return nil, err
	}
	thumbnail := strings.TrimSpace(view.Data.Picture)
	if strings.HasPrefix(thumbnail, "http://") {
		thumbnail = "https://" + strings.TrimPrefix(thumbnail, "http://")
	}
	return &ResolvedMedia{
		DirectURL: directURL,
		Title:     view.Data.Title,
		Ext:       "mp4",
		ID:        view.Data.BVID,
		Uploader:  view.Data.Owner.Name,
		Duration:  view.Data.Duration,
		Thumbnail: thumbnail,
		UserAgent: socialUserAgent,
		Referer:   "https://www.bilibili.com/",
		SkipSleep: true,
	}, nil
}

func bilibiliQuality(maxHeight int) int {
	switch {
	case maxHeight <= 0:
		return 64
	case maxHeight <= 360:
		return 16
	case maxHeight <= 480:
		return 32
	case maxHeight <= 720:
		return 64
	case maxHeight <= 1080:
		return 80
	default:
		return 120
	}
}

func resolveTruthSocial(ctx context.Context, parsed *url.URL, client *http.Client) (*ResolvedMedia, error) {
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	id := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if _, err := strconv.ParseUint(parts[i], 10, 64); err == nil {
			id = parts[i]
			break
		}
	}
	if id == "" {
		return nil, fmt.Errorf("no video could be found in this Truth Social URL")
	}

	endpoint := "https://truthsocial.com/api/v1/statuses/" + id
	body, err := fetchSocialBody(ctx, client, endpoint, "application/json")
	if err != nil && strings.Contains(err.Error(), "HTTP error 403") {
		body, err = fetchSocialBodyViaWget(ctx, endpoint, "application/json")
	}
	if err != nil {
		return nil, err
	}

	var status truthStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("Truth Social returned invalid metadata: %w", err)
	}

	attachmentIndex := -1
	for i, attachment := range status.MediaAttachments {
		if attachment.Type == "video" || attachment.Type == "gifv" {
			attachmentIndex = i
			break
		}
	}
	if attachmentIndex == -1 {
		return nil, fmt.Errorf("no video could be found in this Truth Social post")
	}

	attachment := status.MediaAttachments[attachmentIndex]
	if err := validateDirectMediaURL(attachment.URL); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(status.Title)
	if title == "" {
		title = strings.TrimSpace(html.UnescapeString(socialHTMLTagRe.ReplaceAllString(status.Content, " ")))
		title = strings.Join(strings.Fields(title), " ")
	}
	if title == "" {
		title = "Truth Social video"
	}
	uploader := strings.TrimSpace(status.Account.DisplayName)
	if uploader == "" {
		uploader = status.Account.Username
	}

	return &ResolvedMedia{
		DirectURL: attachment.URL,
		Title:     title,
		Ext:       mediaExtension(attachment.URL, "mp4"),
		ID:        status.ID,
		Uploader:  uploader,
		Duration:  attachment.Meta.Original.Duration,
		Thumbnail: attachment.PreviewURL,
	}, nil
}

func fetchSocialBodyViaWget(ctx context.Context, rawURL, accept string) ([]byte, error) {
	binary := os.Getenv("WGET_BIN")
	if binary == "" {
		binary = "wget"
	}
	cmd := exec.CommandContext(ctx, binary,
		"-qO-",
		"--timeout=30",
		"--tries=1",
		"--max-redirect=5",
		"--user-agent="+socialUserAgent,
		"--header=Accept: "+accept,
		rawURL,
	)
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fallback request failed: %w", err)
	}
	if len(body) > 8<<20 {
		return nil, fmt.Errorf("fallback response is too large")
	}
	return body, nil
}

func resolveRumble(ctx context.Context, rawURL string, maxHeight int, client *http.Client) (*ResolvedMedia, error) {
	endpoint := "https://rumble.com/api/Media/oembed.json?url=" + url.QueryEscape(rawURL)
	body, err := fetchSocialBody(ctx, client, endpoint, "application/json")
	if err != nil {
		return nil, err
	}

	var metadata rumbleOEmbed
	if err := json.Unmarshal(body, &metadata); err != nil {
		return nil, fmt.Errorf("Rumble returned invalid metadata: %w", err)
	}
	match := rumbleIframeRe.FindStringSubmatch(metadata.HTML)
	if len(match) < 2 {
		return nil, fmt.Errorf("no video could be found in this Rumble post")
	}
	embedURL := html.UnescapeString(match[1])
	embedBody, err := fetchSocialBody(ctx, client, embedURL, "text/html")
	if err != nil {
		return nil, err
	}

	uaObject, err := extractJSONObjectAfter(embedBody, `"ua":`)
	if err != nil {
		return nil, fmt.Errorf("no video could be found in this Rumble post")
	}
	var formats struct {
		MP4 map[string]rumbleFormat `json:"mp4"`
	}
	if err := json.Unmarshal(uaObject, &formats); err != nil {
		return nil, fmt.Errorf("Rumble returned invalid video formats: %w", err)
	}
	selected, err := selectRumbleFormat(formats.MP4, maxHeight)
	if err != nil {
		return nil, err
	}
	if err := validateDirectMediaURL(selected.URL); err != nil {
		return nil, err
	}

	embedParsed, _ := url.Parse(embedURL)
	id := strings.Trim(path.Base(strings.TrimSuffix(embedParsed.Path, "/")), "/")
	return &ResolvedMedia{
		DirectURL: selected.URL,
		Title:     metadata.Title,
		Ext:       "mp4",
		ID:        id,
		Uploader:  metadata.AuthorName,
		Duration:  metadata.Duration,
		Thumbnail: metadata.ThumbnailURL,
	}, nil
}

func fetchSocialBody(ctx context.Context, client *http.Client, rawURL, accept string) ([]byte, error) {
	return fetchSocialBodyWithReferer(ctx, client, rawURL, accept, "")
}

func fetchSocialBodyWithReferer(ctx context.Context, client *http.Client, rawURL, accept, referer string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", socialUserAgent)
	request.Header.Set("Accept", accept)
	if referer != "" {
		request.Header.Set("Referer", referer)
	} else if strings.HasSuffix(strings.ToLower(request.URL.Hostname()), ".bilibili.com") || strings.EqualFold(request.URL.Hostname(), "bilibili.com") {
		request.Header.Set("Referer", "https://www.bilibili.com/")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP error %d from %s", response.StatusCode, request.URL.Hostname())
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func extractJSONObjectAfter(body []byte, marker string) ([]byte, error) {
	text := string(body)
	markerIndex := strings.Index(text, marker)
	if markerIndex == -1 {
		return nil, fmt.Errorf("marker not found")
	}
	startOffset := markerIndex + len(marker)
	start := strings.IndexByte(text[startOffset:], '{')
	if start == -1 {
		return nil, fmt.Errorf("object not found")
	}
	start += startOffset
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		char := text[i]
		if inString {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(text[start : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("unterminated object")
}

func selectRumbleFormat(formats map[string]rumbleFormat, maxHeight int) (rumbleFormat, error) {
	type candidate struct {
		height int
		format rumbleFormat
	}
	var candidates []candidate
	for label, format := range formats {
		height := format.Meta.Height
		if height == 0 {
			height, _ = strconv.Atoi(label)
		}
		if height > 0 && format.URL != "" {
			candidates = append(candidates, candidate{height: height, format: format})
		}
	}
	if len(candidates) == 0 {
		return rumbleFormat{}, fmt.Errorf("no video formats found for this Rumble post")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].height < candidates[j].height
	})
	if maxHeight <= 0 {
		return candidates[len(candidates)-1].format, nil
	}
	selected := candidates[0].format
	for _, candidate := range candidates {
		if candidate.height > maxHeight {
			break
		}
		selected = candidate.format
	}
	return selected, nil
}

func validateDirectMediaURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("invalid direct media URL")
	}
	return nil
}

func mediaExtension(rawURL, fallback string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fallback
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(parsed.Path)), ".")
	if ext == "" {
		return fallback
	}
	return ext
}
