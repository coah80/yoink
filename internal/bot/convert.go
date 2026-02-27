package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) handleConvert(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	var attachmentID, format string
	for _, opt := range data.Options {
		switch opt.Name {
		case "file":
			if v, ok := opt.Value.(string); ok {
				attachmentID = v
			}
		case "format":
			format = opt.StringValue()
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
		log.Printf("[Bot] Failed to defer convert response: %v", err)
		return
	}

	go b.processConvert(s, i, attachment, format)
}

func (b *Bot) processConvert(s *discordgo.Session, i *discordgo.InteractionCreate, attachment *discordgo.MessageAttachment, format string) {
	editEmbed(s, i, progressEmbed("Converting...", 0, "", "", fmt.Sprintf("Converting %s to %s", attachment.Filename, format)))

	jobID, err := b.api.startBotConvert(attachment.URL, format)
	if err != nil {
		editEmbed(s, i, errorEmbed("Conversion Failed", err.Error()))
		return
	}

	status, err := b.pollJob(s, i, jobID, false)
	if err != nil {
		editEmbed(s, i, errorEmbed("Conversion Failed", err.Error()))
		return
	}

	if status.Status != "complete" {
		editEmbed(s, i, errorEmbed("Conversion Failed", "Conversion timed out"))
		return
	}

	fileName := status.FileName
	if fileName == "" {
		fileName = status.OutputFilename
	}
	if fileName == "" {
		fileName = fmt.Sprintf("converted_%d.%s", time.Now().Unix(), format)
	}

	fileSize := status.FileSize
	if fileSize > 0 && fileSize <= maxDiscordFileSize && status.DownloadToken != "" {
		fileData, dlName, err := b.api.downloadFile(status.DownloadToken)
		if err != nil {
			log.Printf("[Bot] Convert download failed: %v", err)
			downloadURL := b.api.getDownloadURL(status.DownloadToken)
			editEmbed(s, i, successEmbed("Converted", fileName, fileSize, downloadURL))
			return
		}
		if dlName != "" {
			fileName = dlName
		}
		editWithFile(s, i, successEmbed("Converted", fileName, int64(len(fileData)), ""), fileName, fileData)
		return
	}

	downloadURL := b.api.getDownloadURL(status.DownloadToken)
	editEmbed(s, i, successEmbed("Converted", fileName, fileSize, downloadURL))
}
