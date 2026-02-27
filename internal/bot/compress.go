package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) handleCompress(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	var attachmentID string
	targetMB := 25
	preset := "balanced"

	for _, opt := range data.Options {
		switch opt.Name {
		case "file":
			if v, ok := opt.Value.(string); ok {
				attachmentID = v
			}
		case "target_mb":
			targetMB = int(opt.IntValue())
		case "preset":
			preset = opt.StringValue()
		}
	}

	attachment, ok := data.Resolved.Attachments[attachmentID]
	if !ok || attachment == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{errorEmbed("Error", "No file attached")},
			},
		})
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("[Bot] Failed to defer compress response: %v", err)
		return
	}

	go b.processCompress(s, i, attachment, targetMB, preset)
}

func (b *Bot) processCompress(s *discordgo.Session, i *discordgo.InteractionCreate, attachment *discordgo.MessageAttachment, targetMB int, preset string) {
	editEmbed(s, i, progressEmbed("Compressing...", 0, "", "", fmt.Sprintf("Compressing %s to %dMB", attachment.Filename, targetMB)))

	jobID, err := b.api.startBotCompress(attachment.URL, targetMB, preset)
	if err != nil {
		editEmbed(s, i, errorEmbed("Compression Failed", err.Error()))
		return
	}

	status, err := b.pollJob(s, i, jobID, false)
	if err != nil {
		editEmbed(s, i, errorEmbed("Compression Failed", err.Error()))
		return
	}

	if status.Status != "complete" {
		editEmbed(s, i, errorEmbed("Compression Failed", "Compression timed out"))
		return
	}

	fileName := status.FileName
	if fileName == "" {
		fileName = status.OutputFilename
	}
	if fileName == "" {
		fileName = fmt.Sprintf("compressed_%d.mp4", time.Now().Unix())
	}

	fileSize := status.FileSize
	if fileSize > 0 && fileSize <= maxDiscordFileSize && status.DownloadToken != "" {
		fileData, dlName, err := b.api.downloadFile(status.DownloadToken)
		if err != nil {
			log.Printf("[Bot] Compress download failed: %v", err)
			downloadURL := b.api.getDownloadURL(status.DownloadToken)
			editEmbed(s, i, successEmbed("Compressed", fileName, fileSize, downloadURL))
			return
		}
		if dlName != "" {
			fileName = dlName
		}
		editWithFile(s, i, successEmbed("Compressed", fileName, int64(len(fileData)), ""), fileName, fileData)
		return
	}

	downloadURL := b.api.getDownloadURL(status.DownloadToken)
	editEmbed(s, i, successEmbed("Compressed", fileName, fileSize, downloadURL))
}
