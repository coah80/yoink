package bot

import (
	"bytes"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

const maxDiscordFileSize = 25 * 1024 * 1024

func (b *Bot) handleYoink(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	rawURL := ""
	format := "mp4"

	for _, opt := range data.Options {
		switch opt.Name {
		case "url":
			rawURL = opt.StringValue()
		case "format":
			format = opt.StringValue()
		}
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("[Bot] Failed to defer yoink response: %v", err)
		return
	}

	go b.processYoink(s, i, rawURL, format)
}

func (b *Bot) processYoink(s *discordgo.Session, i *discordgo.InteractionCreate, rawURL, format string) {
	url := normalizeURL(rawURL)
	isPlaylist := isPlaylistURL(url)

	editEmbed(s, i, progressEmbed("Downloading...", 0, "", "", url))

	apiFormat := "video"
	container := "mp4"
	audioFormat := "mp3"
	quality := "1080p"

	switch format {
	case "mp3":
		apiFormat = "audio"
		audioFormat = "mp3"
	case "gif":
		apiFormat = "video"
		container = "mp4"
	case "compressed":
		apiFormat = "video"
		container = "mp4"
		quality = "720p"
	default:
		container = format
	}

	var jobID string
	var err error

	if isPlaylist {
		jobID, err = b.api.startPlaylistDownload(url, apiFormat, quality, container, audioFormat)
	} else {
		jobID, err = b.api.startDownload(url, apiFormat, quality, container, audioFormat, false)
	}
	if err != nil {
		editEmbed(s, i, errorEmbed("Download Failed", err.Error()))
		return
	}

	status, err := b.pollJob(s, i, jobID, isPlaylist)
	if err != nil {
		editEmbed(s, i, errorEmbed("Download Failed", err.Error()))
		return
	}

	if status.Status != "complete" {
		editEmbed(s, i, errorEmbed("Download Failed", "Download timed out"))
		return
	}

	if isPlaylist {
		b.handlePlaylistComplete(s, i, status)
		return
	}

	fileSize := status.FileSize
	fileName := status.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("yoink_%d.%s", time.Now().Unix(), format)
	}

	if fileSize > maxDiscordFileSize {
		editEmbed(s, i, progressEmbed("Compressing...", 0, "", "", "File too large for Discord, auto-compressing..."))

		compressJobID, err := b.api.startBotCompressFromToken(status.DownloadToken, 24)
		if err != nil {
			downloadURL := b.api.getDownloadURL(status.DownloadToken)
			editEmbed(s, i, successEmbed("Yoinked", fileName, fileSize, downloadURL))
			return
		}

		compressStatus, err := b.pollJob(s, i, compressJobID, false)
		if err != nil || compressStatus.Status != "complete" {
			downloadURL := b.api.getDownloadURL(status.DownloadToken)
			editEmbed(s, i, successEmbed("Yoinked", fileName, fileSize, downloadURL))
			return
		}

		status = compressStatus
		fileSize = compressStatus.FileSize
		fileName = compressStatus.FileName
		if fileName == "" && compressStatus.OutputFilename != "" {
			fileName = compressStatus.OutputFilename
		}
	}

	if fileSize > 0 && fileSize <= maxDiscordFileSize && status.DownloadToken != "" {
		fileData, dlName, err := b.api.downloadFile(status.DownloadToken)
		if err != nil {
			log.Printf("[Bot] File download failed: %v", err)
			downloadURL := b.api.getDownloadURL(status.DownloadToken)
			editEmbed(s, i, successEmbed("Yoinked", fileName, fileSize, downloadURL))
			return
		}
		if dlName != "" {
			fileName = dlName
		}

		editWithFile(s, i, successEmbed("Yoinked", fileName, int64(len(fileData)), ""), fileName, fileData)
		return
	}

	downloadURL := b.api.getDownloadURL(status.DownloadToken)
	editEmbed(s, i, successEmbed("Yoinked", fileName, fileSize, downloadURL))
}

func (b *Bot) handlePlaylistComplete(s *discordgo.Session, i *discordgo.InteractionCreate, status *jobStatusResponse) {
	downloadURL := b.api.getDownloadURL(status.DownloadToken)

	embed := successEmbed("Playlist Ready", status.FileName, status.FileSize, downloadURL)

	videoCount := status.VideosComp
	if videoCount == 0 {
		videoCount = status.TotalVideos
	}
	embed.Description = fmt.Sprintf("Your playlist with %d videos is ready!\n\n**This link expires in 5 minutes. Download now.**", videoCount)

	if len(status.FailedVideos) > 0 {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  "Failed Videos",
			Value: fmt.Sprintf("%d video(s) failed to download", len(status.FailedVideos)),
		})
	}

	editEmbed(s, i, embed)
}

func (b *Bot) pollJob(s *discordgo.Session, i *discordgo.InteractionCreate, jobID string, isPlaylist bool) (*jobStatusResponse, error) {
	maxAttempts := 90
	if isPlaylist {
		maxAttempts = 300
	}

	var lastProgress float64 = -1
	lastMessage := ""

	for attempt := 0; attempt < maxAttempts; attempt++ {
		status, err := b.api.checkStatus(jobID)
		if err != nil {
			return nil, err
		}

		if status.Status == "complete" {
			return status, nil
		}
		if status.Status == "error" {
			msg := status.Message
			if msg == "" {
				msg = status.Error
			}
			if msg == "" {
				msg = "Download failed"
			}
			return nil, fmt.Errorf("%s", msg)
		}

		progress := status.Progress
		currentMessage := status.Message

		if progress != lastProgress || currentMessage != lastMessage {
			lastProgress = progress
			lastMessage = currentMessage

			stage := "Working"
			switch status.Status {
			case "downloading":
				stage = "Downloading"
			case "processing":
				stage = "Processing"
			case "compressing":
				stage = "Compressing"
			}

			if isPlaylist && status.TotalVideos > 0 {
				playlistTitle := ""
				if m, ok := status.PlaylistInfo.(map[string]interface{}); ok {
					if t, ok := m["title"].(string); ok {
						playlistTitle = t
					}
				}
				embed := playlistProgressEmbed(
					stage+"...",
					status.VideosComp,
					status.TotalVideos,
					progress,
					len(status.FailedVideos),
					playlistTitle,
				)
				editEmbed(s, i, embed)
			} else {
				embed := progressEmbed(stage+"...", progress, status.Speed, status.ETA, currentMessage)
				editEmbed(s, i, embed)
			}
		}

		delay := pollDelay(attempt, isPlaylist)
		time.Sleep(delay)
	}

	return nil, fmt.Errorf("timed out waiting for job to complete")
}

func pollDelay(attempt int, isPlaylist bool) time.Duration {
	if isPlaylist {
		return 3 * time.Second
	}
	if attempt < 5 {
		return 500 * time.Millisecond
	}
	if attempt < 15 {
		return 1500 * time.Millisecond
	}
	return 3 * time.Second
}

func editEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func editWithFile(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, filename string, data []byte) {
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
		Files: []*discordgo.File{
			{
				Name:   filename,
				Reader: bytes.NewReader(data),
			},
		},
	})
}
