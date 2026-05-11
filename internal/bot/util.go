package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// parseDuration parses a human-readable duration string like "1h30m", "2d", "1d12h".
// Supports d(ays), h(ours), m(inutes), s(econds).
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	re := durationRegex
	matches := re.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid duration format: %s (use e.g. 1h30m, 2d, 1d12h)", s)
	}

	var total time.Duration
	for _, match := range matches {
		val, _ := strconv.Atoi(match[1])
		switch match[2] {
		case "d":
			total += time.Duration(val) * 24 * time.Hour
		case "h":
			total += time.Duration(val) * time.Hour
		case "m":
			total += time.Duration(val) * time.Minute
		case "s":
			total += time.Duration(val) * time.Second
		}
	}

	if total <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return total, nil
}

// formatDuration formats a time.Duration to a human-readable string.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	parts := []string{}
	if days := int(d.Hours()) / 24; days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
		d -= time.Duration(days) * 24 * time.Hour
	}
	if hours := int(d.Hours()); hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		d -= time.Duration(hours) * time.Hour
	}
	if mins := int(d.Minutes()); mins > 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return strings.Join(parts, "")
}

// hasPermission checks if the interaction member has the given permission.
func hasPermission(i *discordgo.InteractionCreate, perm int64) bool {
	if i.Member == nil {
		return false
	}
	return i.Member.Permissions&perm != 0
}

// respondEphemeral sends a quick ephemeral message response.
func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// respondEphemeralEmbed sends a quick ephemeral embed response.
func respondEphemeralEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

// respondEmbed sends an embed response (not ephemeral).
func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// getOptionString extracts a string option by name, returns empty string if missing.
func getOptionString(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, opt := range opts {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}

// getOptionInt extracts an integer option by name, returns 0 if missing.
func getOptionInt(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) int64 {
	for _, opt := range opts {
		if opt.Name == name {
			return opt.IntValue()
		}
	}
	return 0
}

// getOptionUser extracts a user option by name, returns nil if missing.
func getOptionUser(opts []*discordgo.ApplicationCommandInteractionDataOption, name string, resolved *discordgo.ApplicationCommandInteractionDataResolved) *discordgo.User {
	for _, opt := range opts {
		if opt.Name == name {
			if resolved != nil {
				id, ok := opt.Value.(string)
				if !ok {
					return nil
				}
				if u, ok := resolved.Users[id]; ok {
					return u
				}
			}
		}
	}
	return nil
}

// getOptionChannel extracts a channel option by name, returns nil if missing.
func getOptionChannel(opts []*discordgo.ApplicationCommandInteractionDataOption, name string, resolved *discordgo.ApplicationCommandInteractionDataResolved) *discordgo.Channel {
	for _, opt := range opts {
		if opt.Name == name {
			if resolved != nil {
				id, ok := opt.Value.(string)
				if !ok {
					return nil
				}
				if ch, ok := resolved.Channels[id]; ok {
					return ch
				}
			}
		}
	}
	return nil
}

// getOptionRole extracts a role option by name, returns nil if missing.
func getOptionRole(opts []*discordgo.ApplicationCommandInteractionDataOption, name string, resolved *discordgo.ApplicationCommandInteractionDataResolved) *discordgo.Role {
	for _, opt := range opts {
		if opt.Name == name {
			if resolved != nil {
				id, ok := opt.Value.(string)
				if !ok {
					return nil
				}
				if r, ok := resolved.Roles[id]; ok {
					return r
				}
			}
		}
	}
	return nil
}

var durationRegex = regexp.MustCompile(`(\d+)\s*([dhms])`)

const colorMod = 0xFEE75C // Yellow for moderation

// requireGuild checks that an interaction is in a guild and returns false (with error response) if not.
func requireGuild(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	if i.Member == nil || i.GuildID == "" {
		respondEphemeral(s, i, "This command can only be used in a server.")
		return false
	}
	return true
}
