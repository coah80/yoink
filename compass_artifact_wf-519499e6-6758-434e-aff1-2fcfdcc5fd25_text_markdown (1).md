# FFmpeg encoding settings for maximum compression at native resolution

**CRF encoding with the "slow" preset delivers the best quality-per-byte for Discord uploads**, outperforming two-pass encoding when exact file sizes aren't required. For yoink.tools, the optimal approach combines CRF-based quality tiers for user simplicity with two-pass fallback for hitting specific Discord limits. The key insight from encoding communities: CRF 23 with preset "slow" offers **99.5% of maximum achievable quality** while encoding 10x faster than the theoretical optimum.

---

## CRF encoding fundamentals: the quality-focused approach

CRF (Constant Rate Factor) is libx264's quality-based rate control mode, ranging from **0 (lossless) to 51 (worst)**. The critical threshold the encoding community agrees on: **CRF 18 is "visually transparent"**—indistinguishable from the source in normal viewing. Each ±6 CRF change approximately doubles or halves file size.

| CRF Value | Quality Level | Typical Use Case |
|-----------|---------------|------------------|
| 18 | Visually lossless | Archival, short clips where size doesn't matter |
| 20-22 | Excellent | High-quality Discord uploads |
| **23** | Very good (default) | General purpose, solid balance |
| 24-26 | Good | Longer videos needing compression |
| 28+ | Acceptable | Heavy compression, visible quality loss |

**CRF vs two-pass for Discord**: When exact file size isn't required, CRF produces slightly better quality because it doesn't need to adjust for bitrate mismatches. However, Discord's strict upload limits (10MB, 50MB, etc.) often make two-pass necessary for predictable results. The community consensus from VideoHelp and Doom9: "At the same file size, CRF and two-pass produce virtually identical quality—the difference is control, not quality."

The practical recommendation: **Use CRF for quality-focused compression when the resulting file will likely fit your limit**, then fall back to two-pass ABR when you need to hit an exact target.

---

## Preset selection: slow is the sweet spot

The x264 preset controls encoding complexity through dozens of parameters—reference frames, motion estimation algorithms, B-frame decisions, and more. Real-world benchmarks reveal surprising truths about the quality/speed tradeoff.

**File size at same CRF** (benchmark data from encoding communities):
- Ultrafast: 32.8 MB (baseline)
- Medium: 16.9 MB (**48% smaller than ultrafast**)
- Slow: 16.6 MB (1.8% smaller than medium)
- Veryslow: 15.3 MB (**9.5% smaller than medium**)
- Placebo: 15.7 MB (actually *larger* than veryslow in many tests)

**Encoding speed relative to medium**:
- Veryfast: ~6x faster
- Medium: baseline (1x)
- Slow: ~1.5x slower
- Veryslow: ~3-4x slower
- Placebo: ~10-15x slower

The critical finding: **"slow" delivers 99.52% of veryslow's quality in one-third the time**, making it the optimal choice for quality-focused encoding. The Streaming Learning Center's VMAF analysis found placebo often produces *lower quality* than veryslow while taking 4x longer—a unanimous "never use placebo" consensus across encoding forums.

### Tune options for specific content

| Tune | Best For | Effect |
|------|----------|--------|
| **film** | Live action, general content | Reduces deblocking, adds psychovisual optimization |
| **animation** | 2D cartoons, anime with flat colors | More reference frames, optimized for large flat areas |
| **grain** | Grainy sources (film, intentional grain) | Preserves grain structure, prevents splotchy artifacts |
| none | Game clips, screen recordings | Standard settings work well |

**Important**: Animation tune removes grain—avoid it on grainy anime. Use `-tune film` as the safe default for any content.

---

## Native resolution compression: maximizing quality without scaling

Maintaining native resolution while achieving maximum compression requires exploiting encoder efficiency rather than resolution reduction. The key techniques:

**1. Use slower presets at same CRF**: The 10% file size reduction from medium to veryslow comes from better motion estimation and prediction—essentially "smarter" compression that finds redundancy the faster presets miss.

**2. Match CRF to resolution**: Higher resolutions can tolerate higher CRF values because artifacts are less visible at greater pixel density:
- 480p/SD: CRF 18-22
- 720p: CRF 19-23
- 1080p: CRF 20-24
- 4K: CRF 22-28

**3. Content-aware tuning**: Animation content compresses ~25% better than live action at equivalent perceived quality due to flat color areas. Apply `-tune animation` for cartoons to exploit this.

**When to downscale instead**: If your calculated bitrate for a Discord limit falls below **1000 kbps for 1080p** or **500 kbps for 720p**, the quality degradation from compression artifacts will likely exceed the quality loss from a clean downscale. At that point, scaling to 720p with Lanczos filtering (`-vf "scale=-2:720:flags=lanczos"`) produces better perceived quality.

---

## Codec comparison: VP9 beats H.264 but compatibility matters

Netflix's large-scale codec study and community testing establish clear efficiency rankings:

| Codec | File Size vs H.264 | Encoding Speed | Discord Support |
|-------|-------------------|----------------|-----------------|
| **H.264 (x264)** | Baseline | 1x | ✅ Universal |
| **H.265 (x265)** | 35-50% smaller | 10-20x slower | ⚠️ Partial (needs `-tag:v hvc1` for Apple) |
| **VP9** | 35-50% smaller | 10-20x slower | ✅ Full (WebM container) |
| **AV1** | 50% smaller | 50-100x slower | ❌ Upload playback unsupported |

**For yoink.tools, VP9 in WebM is the optimal choice** when encoding speed isn't critical—it matches H.265's efficiency with full Discord compatibility across all platforms. The community recommendation: "Use VP9 at 25-40% of H.264's bitrate with Opus audio at 40-56 kbps."

However, **H.264 remains the safest default** for maximum compatibility, especially when videos may be downloaded and shared beyond Discord. H.265 works but requires the `-tag:v hvc1` flag for Apple devices and has no Linux playback support.

---

## Discord-specific encoding settings

### Essential compatibility flags
Every Discord encode should include:
```
-pix_fmt yuv420p        # Required for universal playback
-movflags +faststart    # Enables streaming before full download
-profile:v high         # Good compatibility
-level 4.2              # Wide device support
```

### Bitrate calculation for target file size
The formula for two-pass encoding:
```
Video_Bitrate_kbps = (Target_Size_MB × 8192) / Duration_Seconds - Audio_Bitrate_kbps
```

**Example**: 10MB target, 60-second video, 96kbps audio = **(10 × 8192) / 60 - 96 = 1269 kbps** video bitrate.

### Audio recommendations
Audio typically represents only 5-10% of file size, so aggressive video compression matters more. Community-tested settings:
- **96 kbps AAC stereo**: Sweet spot for general content
- **64 kbps AAC mono**: Voice/speech content (`-ac 1`)
- **128 kbps AAC**: When audio quality matters

---

## Recommended preset tiers for yoink.tools

Based on community battle-tested settings and the research above, here's how to structure user-facing presets:

### Fast tier (speed priority)
```bash
ffmpeg -i input.mp4 -c:v libx264 -crf 26 -preset veryfast \
  -c:a aac -b:a 96k -pix_fmt yuv420p -movflags +faststart output.mp4
```
- **Speed**: ~6x faster than balanced
- **Quality**: Good (visible compression on scrutiny)
- **File size**: ~120% of balanced tier
- **Use case**: Quick previews, bulk encoding, impatient users

### Balanced tier (recommended default)
```bash
ffmpeg -i input.mp4 -c:v libx264 -crf 23 -preset medium \
  -c:a aac -b:a 128k -pix_fmt yuv420p -movflags +faststart output.mp4
```
- **Speed**: Baseline
- **Quality**: Very good (minimal visible loss)
- **File size**: Baseline
- **Use case**: General purpose, good tradeoff

### Quality tier (maximum compression efficiency)
```bash
ffmpeg -i input.mp4 -c:v libx264 -crf 20 -preset slow \
  -c:a aac -b:a 128k -pix_fmt yuv420p -movflags +faststart output.mp4
```
- **Speed**: ~1.5x slower than balanced
- **Quality**: Excellent (near-transparent)
- **File size**: ~85-90% of balanced tier
- **Use case**: Quality-focused users, final exports

### Target size mode (two-pass for exact limits)
```bash
# Pass 1
ffmpeg -y -i input.mp4 -c:v libx264 -b:v ${CALCULATED_BITRATE}k \
  -preset medium -pass 1 -an -f mp4 /dev/null

# Pass 2  
ffmpeg -i input.mp4 -c:v libx264 -b:v ${CALCULATED_BITRATE}k \
  -preset medium -pass 2 -c:a aac -b:a 96k \
  -pix_fmt yuv420p -movflags +faststart output.mp4
```

### VP9 high-compression mode (best quality/size)
```bash
ffmpeg -i input.mp4 -c:v libvpx-vp9 -crf 30 -b:v 0 -preset slow \
  -c:a libopus -b:a 64k output.webm
```
- **Speed**: 10-20x slower than H.264
- **Quality**: Excellent
- **File size**: ~50-65% of H.264 balanced tier
- **Use case**: Maximum compression when time permits

---

## Content-specific optimizations

| Content Type | Recommended Settings | Notes |
|--------------|---------------------|-------|
| **Game clips** | CRF 22-24, no tune | High motion handles compression well |
| **Screen recordings** | CRF 24-26, no tune | Low motion compresses excellently |
| **Animation/anime** | CRF 20-22, `-tune animation` | ~25% better compression |
| **Film/live action** | CRF 22-24, `-tune film` | Standard approach |
| **Grainy content** | CRF 20-22, `-tune grain` | Prevents splotchy artifacts |

---

## Conclusion

For yoink.tools, the optimal strategy combines **CRF-based encoding for quality tiers** with **two-pass fallback for hitting exact Discord limits**. The "slow" preset with CRF 20-23 represents the efficiency sweet spot—slower presets offer diminishing returns while faster presets sacrifice meaningful quality.

The surprising finding: **VP9 in WebM delivers H.265-level compression with full Discord compatibility**, making it the best choice when encoding speed isn't critical. For maximum compatibility, stick with H.264 but always include `-pix_fmt yuv420p -movflags +faststart`.

Key numbers to remember: CRF 18 is visually lossless, CRF 23 is the sensible default, ±6 CRF doubles/halves file size, and the "slow" preset achieves 99.5% of maximum quality in one-third the time of theoretical best.