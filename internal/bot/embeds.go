package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	colorProgress = 0x5865F2
	colorSuccess  = 0x57F287
	colorError    = 0xED4245
)

func progressBar(percent float64) string {
	filled := int(percent / 10)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("\u2593", filled) + strings.Repeat("\u2591", 10-filled)
}

func formatSize(bytes int64) string {
	if bytes <= 0 {
		return "Unknown"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

func progressEmbed(title string, progress float64, speed, eta, message string) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("%s %d%%", progressBar(progress), int(progress))

	details := []string{}
	if speed != "" {
		details = append(details, speed)
	}
	if eta != "" {
		details = append(details, "~"+eta+" left")
	}
	if len(details) > 0 {
		desc += " \u00b7 " + strings.Join(details, " \u00b7 ")
	}
	if message != "" {
		desc += "\n" + message
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: desc,
		Color:       colorProgress,
		Footer:      &discordgo.MessageEmbedFooter{Text: "yoink.tools"},
	}
}

func playlistProgressEmbed(title string, current, total int, progress float64, failed int, playlistTitle string) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("%s %d%%", progressBar(progress), int(progress))
	desc += fmt.Sprintf("\n\n**Progress:** %d/%d videos", current, total)
	if failed > 0 {
		desc += fmt.Sprintf(" (%d failed)", failed)
	}
	if playlistTitle != "" {
		desc += fmt.Sprintf("\n**Playlist:** %s", playlistTitle)
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: desc,
		Color:       colorProgress,
		Footer:      &discordgo.MessageEmbedFooter{Text: "yoink.tools"},
	}
}

func successEmbed(title, filename string, fileSize int64, downloadURL string) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}
	if filename != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "File", Value: filename, Inline: true,
		})
	}
	if fileSize > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Size", Value: formatSize(fileSize), Inline: true,
		})
	}
	if downloadURL != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Download", Value: fmt.Sprintf("[Click here](%s)", downloadURL),
		})
	}

	return &discordgo.MessageEmbed{
		Title:  title,
		Color:  colorSuccess,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{Text: "yoink.tools"},
	}
}

func errorEmbed(title, message string) *discordgo.MessageEmbed {
	if message == "" {
		message = "Something went wrong"
	}
	return &discordgo.MessageEmbed{
		Title:       title,
		Description: message,
		Color:       colorError,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Try a different URL or format"},
	}
}
