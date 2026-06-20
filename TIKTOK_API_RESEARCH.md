# TikTok API Research for Go Extractor

Research date: 2026-04-03. This documents everything needed to build a native Go TikTok
extractor that replaces yt-dlp for TikTok content.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Approach 1: tikwm.com Third-Party API (Primary)](#approach-1-tikwmcom-third-party-api)
3. [Approach 2: Direct Web Scraping](#approach-2-direct-web-scraping)
4. [Approach 3: Mobile App API](#approach-3-mobile-app-api)
5. [TikTok oEmbed API](#tiktok-oembed-api)
6. [Content Types](#content-types)
7. [CDN URL Patterns](#cdn-url-patterns)
8. [Anti-Bot Systems](#anti-bot-systems)
9. [Rate Limiting](#rate-limiting)
10. [Implementation Plan](#implementation-plan)

---

## Architecture Overview

Three viable approaches, in order of reliability and simplicity:

| Approach | Difficulty | Auth Needed | Rate Limit | Reliability |
|----------|-----------|-------------|------------|-------------|
| tikwm.com API | Easy | None | 5000/day, 1/sec | High (third-party) |
| Web scraping + rehydration JSON | Medium | None (challenge solving) | ~100/hr/IP | Medium |
| Mobile app API (aweme/v1/) | Hard | Device ID + signing | Unknown | Highest quality |

**Recommended**: tikwm.com as primary, web scraping as fallback, oEmbed for metadata-only.

---

## Approach 1: tikwm.com Third-Party API

This is what yoink already uses for music. It handles videos, slideshows, and audio with
zero authentication. Battle-tested by tok-dl (Go), heilkit/tt (Go), and many others.

### Base URL

```
https://tikwm.com/api/
```

### Video/Post Detail Endpoint

```
GET https://tikwm.com/api/?url={tiktok_url}
GET https://tikwm.com/api/?url={tiktok_url}&hd=1
```

- `url`: Any TikTok video/post URL (full, shortened, or just video ID)
- `hd=1`: Request HD quality video (different CDN URL, often smaller filesize but higher res)
- No authentication headers required
- `Accept: application/json` header recommended

### Music Posts Endpoint

```
GET https://tikwm.com/api/music/posts/?music_id={music_id}&count={count}
```

- `music_id`: Numeric music/sound ID extracted from URL
- `count`: Number of videos to return (1 is fine for getting the audio URL)

### Video Detail Response Structure (verified live 2026-04-03)

```json
{
  "code": 0,
  "msg": "success",
  "processed_time": 0.3971,
  "data": {
    "id": "6718335390845095173",
    "region": "US",
    "title": "Post caption with #hashtags",

    "cover": "https://p16-common-sign.tiktokcdn-us.com/...",
    "ai_dynamic_cover": "https://p16-common-sign.tiktokcdn-us.com/...",
    "origin_cover": "https://p19-common-sign.tiktokcdn-us.com/...",

    "duration": 10,

    "play": "https://v19.tiktokcdn-us.com/...",
    "wmplay": "https://v19.tiktokcdn-us.com/...",
    "hdplay": "https://v16m-default.tiktokcdn-us.com/...",

    "size": 2953029,
    "wm_size": 0,
    "hd_size": 2004627,

    "music": "https://v16-ies-music.tiktokcdn-us.com/...",
    "music_info": {
      "id": "6689804660171082501",
      "title": "original sound - username",
      "play": "https://v16-ies-music.tiktokcdn-us.com/...",
      "cover": "https://p16-common-sign.tiktokcdn-us.com/...",
      "author": "tiff",
      "original": true,
      "duration": 10,
      "album": ""
    },

    "play_count": 156615,
    "digg_count": 34778,
    "comment_count": 5880,
    "share_count": 1408,
    "download_count": 252,
    "collect_count": 613,
    "create_time": 1564234358,

    "is_ad": false,
    "commerce_info": {
      "adv_promotable": false,
      "auction_ad_invited": false,
      "branded_content_type": 0,
      "with_comment_filter_words": false
    },

    "author": {
      "id": "53279706535428096",
      "unique_id": "scout2015",
      "nickname": "Scout, Suki & Stella",
      "avatar": "https://p16-common-sign.tiktokcdn-us.com/..."
    },

    "images": null
  }
}
```

### Key Video URL Fields

| Field | Description | Watermark | Quality |
|-------|-------------|-----------|---------|
| `data.play` | Standard quality video, no watermark | No | SD/720p |
| `data.hdplay` | HD quality video, no watermark | No | 1080p (when `hd=1`) |
| `data.wmplay` | Watermarked video | Yes | Same as play |
| `data.music` | Audio track URL (direct) | N/A | Original |
| `data.music_info.play` | Same audio via music_info | N/A | Original |

### Slideshow/Carousel Response (verified live 2026-04-03)

For photo/slideshow posts, the response is identical except:

- `duration` = 0
- `play`, `wmplay`, `hdplay` all point to the **audio track** (not video, there is no video)
- `size`, `wm_size`, `hd_size` = 0
- `images` is a **string array** of direct image URLs instead of null

```json
{
  "data": {
    "duration": 0,
    "play": "https://v16-ies-music.tiktokcdn-us.com/...(audio)...",
    "images": [
      "https://p16-common-sign.tiktokcdn-us.com/tos-alisg-i-photomode-sg/...~tplv-photomode-image.jpeg?...",
      "https://p16-common-sign.tiktokcdn-us.com/tos-alisg-i-photomode-sg/...~tplv-photomode-image.jpeg?...",
      "https://p16-common-sign.tiktokcdn-us.com/tos-alisg-i-photomode-sg/...~tplv-photomode-image.jpeg?..."
    ]
  }
}
```

**Detection logic**: A post is a slideshow when `data.images != nil && len(data.images) > 0`.
Alternatively, `data.duration == 0` combined with images present.

### Error Responses

```json
{"code": -1, "msg": "Url parsing is failed! Please check url."}
{"code": -1, "msg": "Free Api Limit reached, please try again later"}
```

Error codes:
- `code: 0` = success
- `code: -1` = error (check msg)
- "Free Api Limit" prefix = rate limited
- "Url parsing is failed" prefix = invalid/unavailable URL

### Rate Limits

- **5,000 requests per day** (free tier)
- **1 request per second** minimum interval
- Rate limit resets daily
- No API key needed

### Go Types (from tok-dl reference implementation)

```go
const BaseURL = "https://tikwm.com/api/"

type TikWMResponse struct {
    Code          int     `json:"code"`
    Msg           string  `json:"msg"`
    ProcessedTime float64 `json:"processed_time"`
    Data          TikWMData `json:"data"`
}

type TikWMData struct {
    ID             string   `json:"id"`
    Region         string   `json:"region"`
    Title          string   `json:"title"`
    Cover          string   `json:"cover"`
    AiDynamicCover string   `json:"ai_dynamic_cover"`
    OriginCover    string   `json:"origin_cover"`
    Duration       int      `json:"duration"`
    Play           string   `json:"play"`
    Hdplay         string   `json:"hdplay"`
    Wmplay         string   `json:"wmplay"`
    Size           int      `json:"size"`
    WmSize         int      `json:"wm_size"`
    HdSize         int      `json:"hd_size"`
    Music          string   `json:"music"`
    MusicInfo      MusicInfo `json:"music_info"`
    PlayCount      int      `json:"play_count"`
    DiggCount      int      `json:"digg_count"`
    CommentCount   int      `json:"comment_count"`
    ShareCount     int      `json:"share_count"`
    DownloadCount  int      `json:"download_count"`
    CollectCount   int      `json:"collect_count"`
    CreateTime     int64    `json:"create_time"`
    IsAd           bool     `json:"is_ad"`
    Author         Author   `json:"author"`
    Images         []string `json:"images"`
}

type MusicInfo struct {
    ID       string `json:"id"`
    Title    string `json:"title"`
    Play     string `json:"play"`
    Cover    string `json:"cover"`
    Author   string `json:"author"`
    Original bool   `json:"original"`
    Duration int    `json:"duration"`
    Album    string `json:"album"`
}

type Author struct {
    ID       string `json:"id"`
    UniqueID string `json:"unique_id"`
    Nickname string `json:"nickname"`
    Avatar   string `json:"avatar"`
}
```

---

## Approach 2: Direct Web Scraping

Scrape the TikTok video page and extract embedded JSON data. This is what yt-dlp does.
More complex but doesn't depend on a third party.

### How It Works

1. Fetch `https://www.tiktok.com/@{user}/video/{id}` with browser-like headers
2. Parse `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">` from HTML
3. Navigate JSON: `__DEFAULT_SCOPE__` -> `webapp.video-detail` -> `itemInfo` -> `itemStruct`
4. Extract video URLs from `itemStruct.video`

### Required Headers

```
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36
Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8
Accept-Language: en-US,en;q=0.5
Referer: https://www.tiktok.com/
```

### Challenge System (WAF)

TikTok now serves a JavaScript challenge page instead of the actual content. yt-dlp handles
this with a native Python solver:

1. First request returns HTML with `<div id="cs" class="{base64_data}">` containing challenge params
2. Challenge is a SHA-256 proof-of-work: brute-force a number 0-1000000 where
   `sha256(base_hash + number) == expected_digest`
3. Set cookies: `_wafchallengeid` = solution, `waforiginalreid` = redirect ID
4. Re-request the page with challenge cookies
5. Cookies expire after ~120 seconds

### Rehydration Data Path

```
__UNIVERSAL_DATA_FOR_REHYDRATION__
  -> __DEFAULT_SCOPE__
    -> webapp.video-detail
      -> statusCode (0 = ok, 10216 = private, 10222 = private account, 10204 = IP blocked)
      -> itemInfo
        -> itemStruct
          -> id
          -> desc (caption)
          -> createTime (unix timestamp)
          -> video (video object)
          -> author (author object)
          -> stats (engagement metrics)
          -> music (sound info)
          -> imagePost (slideshow images -- see below)
```

### itemStruct.video Fields (Web)

```
video.duration         - seconds
video.ratio            - e.g. "720p"
video.width            - pixel width
video.height           - pixel height
video.cover            - thumbnail URL
video.originCover      - full-res thumbnail
video.dynamicCover     - animated thumbnail
video.playAddr         - streaming URL (may be string or array of {src: url})
video.downloadAddr     - download URL (watermarked)
video.bitrateInfo[]    - array of quality variants:
  .PlayAddr.UrlList[]  - URLs for this bitrate
  .PlayAddr.UrlKey     - format key like "v0d00fg10000cr...h264_540p_1234"
  .PlayAddr.DataSize   - file size in bytes
  .Bitrate             - bits per second
  .QualityType         - quality level
  .GearName            - e.g. "normal_540_0"
```

### URL Key Format (from yt-dlp)

Pattern: `v{version}_{codec}_{resolution}_{bitrate}`

- `codec`: h264, bytevc1 (h265), bytevc2 (h266/VVC - unplayable)
- `resolution`: 360p, 540p (actually 576p), 720p, 1080p
- `bitrate`: in kbps

### itemStruct.stats Fields (Web)

```
stats.playCount
stats.diggCount        (likes)
stats.shareCount
stats.commentCount
stats.collectCount     (bookmarks/favorites)
```

### itemStruct.music Fields (Web)

```
music.title
music.authorName
music.album
music.duration
music.playUrl          - direct audio URL (this is the key field)
```

For slideshows (no video), when no video formats are found, the audio comes from
`music.playUrl`. The audio MIME type is encoded in the URL query param `mime_type`
(e.g., `audio_mpeg` -> `audio/mpeg` -> mp3, `audio_mp4` -> m4a).

### Slideshow Data in Web (imagePost)

The web rehydration data uses `imagePost` within `itemStruct` for slideshows. The images
are also found via the aweme_detail path:

```
aweme_detail.image_post_info.images[].thumbnail.url_list[0]  (no watermark)
```

In the web itemStruct, look for the `imagePost` field. If it exists and has images,
it's a slideshow.

### Alternative Web Endpoints

```
GET https://www.tiktok.com/api/post/item_list/
  ?aid=1988
  &count=35
  &cursor=0
  &device_platform=web_pc
  &secUid={user_sec_uid}

GET https://www.tiktok.com/api/comment/list/
  ?aweme_id={video_id}
  &count=20
  &cursor=0

GET https://www.tiktok.com/api/music/detail/
  ?musicId={music_id}
```

These require valid session cookies and are harder to use reliably.

---

## Approach 3: Mobile App API

What yt-dlp calls the "app" path. Highest quality but hardest to use.

### API Hostname

```
api16-normal-c-useast1a.tiktokv.com
```

### Video Detail Endpoint

```
POST https://api16-normal-c-useast1a.tiktokv.com/aweme/v1/multi/aweme/detail/

Body (form-encoded):
  aweme_ids=[{video_id}]
  request_source=0

Headers:
  User-Agent: com.zhiliaoapp.musically/2023501030 (Linux; U; Android 13; en_US; Pixel 7; Build/TD1A.220804.031; Cronet/58.0.2991.0)
  Accept: application/json
  X-Argus: (empty string)
  Cookie: [redacted]
```

### Required Query Parameters (all requests)

```
device_platform=android
os=android
ssmix=a
_rticket={timestamp_ms}
cdid={uuid4}
channel=googleplay
aid=0
app_name=musical_ly
version_code=350103
version_name=35.1.3
manifest_version_code=2023501030
update_version_code=2023501030
ab_version=35.1.3
resolution=1080*2400
dpi=420
device_type=Pixel 7
device_brand=Google
language=en
os_api=29
os_version=13
ac=wifi
is_pad=0
current_region=US
app_type=normal
sys_region=US
last_install_time={timestamp - random(86400,1123200)}
timezone_name=America/New_York
residence=US
app_language=en
timezone_offset=-14400
host_abi=armeabi-v7a
locale=en
ac2=wifi5g
uoo=1
carrier_region=US
op_region=US
build_number=35.1.3
region=US
ts={timestamp}
device_id={random_large_int or known device id}
openudid={random_16_hex_chars}
iid={install_id if known}
```

### Response Structure (aweme_detail)

```
aweme_details[0]:
  aweme_id
  desc
  create_time
  author:
    unique_id, uid, nickname, sec_uid
  statistics:
    play_count, digg_count, comment_count, share_count, collect_count
  video:
    play_addr:
      url_list[], url_key, data_size, width, height
    download_addr:
      url_list[], data_size, width, height
      has_watermark (bool)
    play_addr_h264:
      url_list[]
    play_addr_bytevc1:
      url_list[]
    bit_rate[]:
      play_addr: {url_list[], url_key, data_size}
      bit_rate (bps)
      gear_name
      FPS
      is_bytevc1 / is_h265
    duration (milliseconds)
    width, height
    cover, origin_cover, dynamic_cover, ai_dynamic_cover
  music:
    title, author, album, duration
    play_url: {url_list[]}
    is_original_sound
    owner_handle
    matched_song: {title, author}
    matched_pgc_sound: {title, author}
  image_post_info:        (slideshows only)
    images[]:
      thumbnail:
        url_list[]        (url_list[0] = no watermark)
```

### Signing Requirements

The mobile API requires several signature headers:
- **X-Gorgon**: Primary signing algorithm
- **X-SS-STUB**: MD5 of request body
- **X-Khronos**: Timestamp
- **X-Argus**: Additional signature (can be empty string for some endpoints)
- **X-Ladon**: Optional secondary signature

yt-dlp currently sends `X-Argus: ""` (empty) and it works for the detail endpoint.
The other signatures are NOT currently required for `multi/aweme/detail`.

---

## TikTok oEmbed API

Free, unauthenticated, and rate-limit-friendly. Good for metadata but no download URLs.

### Endpoint

```
GET https://www.tiktok.com/oembed?url={tiktok_url}
```

### Response (verified live)

```json
{
  "version": "1.0",
  "type": "video",
  "title": "Post caption text",
  "author_url": "https://www.tiktok.com/@username",
  "author_name": "Display Name",
  "author_unique_id": "username",
  "width": "100%",
  "height": "100%",
  "html": "<blockquote class=\"tiktok-embed\"...>...</blockquote>",
  "thumbnail_width": 576,
  "thumbnail_height": 1024,
  "thumbnail_url": "https://p16-common-sign.tiktokcdn-us.com/...",
  "provider_url": "https://www.tiktok.com",
  "provider_name": "TikTok",
  "embed_product_id": "6718335390845095173",
  "embed_type": "video"
}
```

Useful for: getting title, author, thumbnail before the full download.
Does NOT provide: download URLs, music info, slideshow images, stats.

---

## Content Types

### Videos

Standard TikTok posts with video content.
- `duration > 0`
- `play` / `hdplay` contain video URLs
- `images` is null/empty

### Slideshows / Photo Carousels

Multi-image posts with background music. TikTok calls these "Photo Mode" posts.
- `duration == 0`
- `play` / `hdplay` / `wmplay` / `music` all point to the **audio track** (same URL)
- `images` is a string array of direct JPEG URLs
- 4-35 images per post
- Image URLs contain `photomode` in the path
- Images are typically high-res JPEG

### Audio / Music

Sound pages with a music ID.
- URL pattern: `tiktok.com/music/{slug}-{music_id}`
- Use `/api/music/posts/` endpoint to get a video using this sound
- Extract the `music` URL from the first result
- Or use `/api/?url=` with a video URL that uses the sound

---

## CDN URL Patterns

### Video CDNs

```
v16.tiktokcdn-us.com        (standard US)
v16m.tiktokcdn-us.com       (mobile US)
v16m-default.tiktokcdn-us.com  (HD US)
v19.tiktokcdn-us.com        (newer US)
v16.tiktokcdn.com           (international)
v19.tiktokcdn.com           (newer international)
```

### Audio/Music CDNs

```
v16-ies-music.tiktokcdn-us.com   (US music)
sf16-ies-music-va.tiktokcdn.com  (international music, older format)
```

### Image CDNs (slideshows)

```
p16-common-sign.tiktokcdn-us.com  (US images)
p19-common-sign.tiktokcdn-us.com  (US images alt)
p16-common-sign.tiktokcdn.com     (international)
```

Image paths contain: `tos-alisg-i-photomode-sg/{hash}~tplv-photomode-image.jpeg`

### Cover/Thumbnail CDNs

```
p16-common-sign.tiktokcdn-us.com  (with tplv-tiktokx-cropcenter params)
```

### URL Expiration

All CDN URLs are **temporary** and expire. Key query parameters:
- `x-expires`: Unix timestamp of expiration (typically hours from now)
- `x-signature`: HMAC signature validating the URL
- `refresh_token`: Token for URL refresh
- `dr`: Unknown numeric parameter
- `t`, `ps`, `shp`, `shcp`: Additional signing params

URLs typically expire within a few hours. Download immediately after fetching.

### Download Headers

For downloading from CDN URLs, set:
```
Referer: https://www.tiktok.com/
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36
```

Some CDN URLs work without any headers. The tikwm-provided URLs tend to be more
permissive than direct TikTok web URLs.

---

## Anti-Bot Systems

### WAF Challenge (Web)

- TikTok serves a JS challenge page requiring SHA-256 proof-of-work
- Cookie-based: set `_wafchallengeid` cookie with solution
- yt-dlp solves this natively in Python (brute-force 0-1M)
- tikwm.com handles this server-side (no concern for us)

### Request Signing (Web API)

- **msToken**: Obtained from cookies after visiting TikTok, validated server-side
- **X-Bogus**: Signs each request using double-MD5 + RC4 with custom alphabet
  - Generated by TikTok's obfuscated VM in the browser
  - Algorithm: MD5(URL params + body), RC4-encrypt UA with key [0,1,14], base64+MD5,
    assemble salt array, filter+scramble+RC4 with key [255], custom base64
- **X-Gnarly**: Additional header signature (combines signed URL + msToken + UA)
- **x-tt-trace-id**: 128-char hex session token required for web API requests

### Device Fingerprinting

- Canvas, WebGL, navigator properties, font signatures
- Not relevant for tikwm.com approach
- Only matters if doing direct web scraping with headless browsers

### Mobile API Signing

- **X-Gorgon**: Mobile request signature (complex algorithm)
- **X-Argus**: Can be empty string for some endpoints (yt-dlp's approach)
- **X-SS-STUB**: MD5 of POST body
- Much harder to replicate than web signing

### Bypass Strategy

The tikwm.com API handles ALL of this server-side. We just send a GET request with the
TikTok URL and get back clean JSON with direct download URLs. This is why it's the
recommended primary approach.

---

## Rate Limiting

### tikwm.com

- 5,000 requests/day (free tier)
- 1 request/second minimum spacing
- Error message prefix: "Free Api Limit"
- No API key, no signup

### TikTok Direct (Web)

- ~100 requests/hour per IP without proxies
- 200-400 requests/day per residential proxy IP (safe range)
- Request timing: randomize 8-25 second delays between requests
- Datacenter IPs get preemptive blocking
- Mobile residential proxies recommended for scale

### TikTok oEmbed

- Generous limits, rarely rate-limited
- Good for metadata prefetch

---

## Implementation Plan

### Phase 1: tikwm.com Extractor (Replaces yt-dlp for TikTok)

The extractor should handle three content types:

1. **Video posts**: Fetch via tikwm, return `hdplay` (or `play` fallback) URL
2. **Slideshow posts**: Fetch via tikwm, return `images[]` array + `music` audio URL
3. **Music/sound pages**: Fetch via tikwm music endpoint, return audio URL

Detection flow:
```
IsTikTokURL(url) -> true
  |
  +--> IsTikTokMusicURL(url) -> DownloadTikTokMusic() [existing]
  |
  +--> FetchTikWMMetadata(url)
         |
         +--> images != nil -> HandleSlideshow(images, music)
         |
         +--> duration > 0  -> HandleVideo(hdplay || play)
         |
         +--> else          -> HandleAudio(music)
```

### Phase 2: Metadata Cache

Use oEmbed for fast metadata (title, author, thumbnail) that doesn't count against
tikwm rate limits. Cache tikwm responses to avoid re-fetching.

### Phase 3: Direct Scraping Fallback

If tikwm.com goes down or rate-limits us:
1. Fetch TikTok page with browser-like headers
2. Solve WAF challenge if needed (SHA-256 proof-of-work, max 1M iterations)
3. Parse `__UNIVERSAL_DATA_FOR_REHYDRATION__` JSON
4. Extract `itemStruct.video.playAddr` or `itemStruct.video.bitrateInfo[].PlayAddr`
5. For slideshows: extract `imagePost` images
6. For audio: extract `music.playUrl`

### File Organization (proposed)

```
internal/services/tiktok/
  extractor.go       - main extraction logic, URL routing
  tikwm.go           - tikwm.com API client
  types.go           - shared types (TikWMResponse, VideoInfo, etc.)
  scraper.go         - direct web scraping fallback
  oembed.go          - oEmbed metadata client
```

---

## Reference Implementations

- **tok-dl** (Go, tikwm): https://github.com/sweepies/tok-dl
- **yt-dlp** (Python, web+mobile): https://github.com/yt-dlp/yt-dlp/blob/master/yt_dlp/extractor/tiktok.py
- **Evil0ctal** (Python, web+signing): https://github.com/Evil0ctal/Douyin_TikTok_Download_API
- **davidteather/TikTok-Api** (Python, web): https://github.com/davidteather/TikTok-Api
- **SyntaxSparkk/TikTok** (docs, mobile): https://github.com/SyntaxSparkk/TikTok
