package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const colorWelcome = 0x2ECC71 // Green

func welcomeCommands() []*discordgo.ApplicationCommand {
	dmPerm := false
	adminPerm := int64(discordgo.PermissionAdministrator)

	return []*discordgo.ApplicationCommand{
		{
			Name:                     "welcome",
			Description:              "Configure welcome messages",
			DefaultMemberPermissions: &adminPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "set",
					Description: "Set welcome channel and message",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Welcome channel", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "message", Description: "Welcome message ({user}, {username}, {server}, {memberCount})", Required: true},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "disable",
					Description: "Disable welcome messages",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "test",
					Description: "Preview the welcome message with yourself",
				},
			},
		},
		{
			Name:                     "autorole",
			Description:              "Configure auto-role for new members",
			DefaultMemberPermissions: &adminPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "set",
					Description: "Set auto-role for new members",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionRole, Name: "role", Description: "Role to assign", Required: true},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "disable",
					Description: "Disable auto-role",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "view",
					Description: "View current auto-role settings",
				},
			},
		},
	}
}

// --- Command Handlers ---

func (b *Bot) handleWelcome(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		respondEphemeral(s, i, "Please specify a subcommand.")
		return
	}

	sub := data.Options[0]
	gs, err := getGuildSettings(b.db, i.GuildID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	switch sub.Name {
	case "set":
		ch := getOptionChannel(sub.Options, "channel", data.Resolved)
		msg := getOptionString(sub.Options, "message")
		if ch == nil {
			respondEphemeral(s, i, "Please specify a channel.")
			return
		}

		b.db.Exec(`UPDATE guild_settings SET welcome_channel = ?, welcome_message = ? WHERE guild_id = ?`,
			ch.ID, msg, gs.GuildID)

		guildName := resolveGuildName(s, i.GuildID)
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "Welcome Message Set",
			Description: fmt.Sprintf("Welcome messages will be sent to <#%s>.\n\n**Preview:**\n%s", ch.ID, replaceWelcomeVars(msg, i.Member.User, guildName, 0)),
			Color:       colorWelcome,
		})

	case "disable":
		b.db.Exec(`UPDATE guild_settings SET welcome_channel = '', welcome_message = '' WHERE guild_id = ?`, gs.GuildID)
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "Welcome Disabled",
			Description: "Welcome messages have been disabled.",
			Color:       colorWelcome,
		})

	case "test":
		if gs.WelcomeChannel == "" || gs.WelcomeMessage == "" {
			respondEphemeral(s, i, "No welcome message configured. Use `/welcome set` first.")
			return
		}

		guild, err := s.Guild(i.GuildID)
		guildName := i.GuildID
		memberCount := 0
		if err == nil {
			guildName = guild.Name
			memberCount = guild.MemberCount
		}

		msg := replaceWelcomeVars(gs.WelcomeMessage, i.Member.User, guildName, memberCount)
		s.ChannelMessageSendEmbed(gs.WelcomeChannel, &discordgo.MessageEmbed{
			Description: msg,
			Color:       colorWelcome,
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: i.Member.User.AvatarURL("256")},
			Timestamp:   time.Now().Format(time.RFC3339),
		})

		respondEphemeral(s, i, fmt.Sprintf("Test welcome message sent to <#%s>.", gs.WelcomeChannel))
	}
}

func (b *Bot) handleAutoRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		respondEphemeral(s, i, "Please specify a subcommand.")
		return
	}

	sub := data.Options[0]
	gs, err := getGuildSettings(b.db, i.GuildID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	switch sub.Name {
	case "set":
		role := getOptionRole(sub.Options, "role", data.Resolved)
		if role == nil {
			respondEphemeral(s, i, "Please specify a role.")
			return
		}

		b.db.Exec(`UPDATE guild_settings SET auto_role = ? WHERE guild_id = ?`, role.ID, gs.GuildID)
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "Auto Role Set",
			Description: fmt.Sprintf("New members will receive the <@&%s> role.", role.ID),
			Color:       colorSuccess,
		})

	case "disable":
		b.db.Exec(`UPDATE guild_settings SET auto_role = '' WHERE guild_id = ?`, gs.GuildID)
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "Auto Role Disabled",
			Description: "Auto-role has been disabled.",
			Color:       colorWelcome,
		})

	case "view":
		if gs.AutoRole == "" {
			respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
				Title:       "Auto Role",
				Description: "No auto-role configured.",
				Color:       colorWelcome,
			})
		} else {
			respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
				Title:       "Auto Role",
				Description: fmt.Sprintf("New members receive: <@&%s>", gs.AutoRole),
				Color:       colorWelcome,
			})
		}
	}
}

// --- Event Handler ---

func (b *Bot) handleGuildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	gs, err := getGuildSettings(b.db, m.GuildID)
	if err != nil {
		log.Printf("[Welcome] Failed to get guild settings: %v", err)
		return
	}

	// Auto-role
	if gs.AutoRole != "" {
		if err := s.GuildMemberRoleAdd(m.GuildID, m.User.ID, gs.AutoRole); err != nil {
			log.Printf("[Welcome] Failed to add auto-role: %v", err)
		}
	}

	// Welcome message
	if gs.WelcomeChannel != "" && gs.WelcomeMessage != "" {
		guild, err := s.Guild(m.GuildID)
		guildName := m.GuildID
		memberCount := 0
		if err == nil {
			guildName = guild.Name
			memberCount = guild.MemberCount
		}

		msg := replaceWelcomeVars(gs.WelcomeMessage, m.User, guildName, memberCount)
		s.ChannelMessageSendEmbed(gs.WelcomeChannel, &discordgo.MessageEmbed{
			Description: msg,
			Color:       colorWelcome,
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: m.User.AvatarURL("256")},
			Timestamp:   time.Now().Format(time.RFC3339),
		})
	}

	// Log to mod log
	if gs.ModLogChannel != "" {
		s.ChannelMessageSendEmbed(gs.ModLogChannel, &discordgo.MessageEmbed{
			Title:       "Member Joined",
			Description: fmt.Sprintf("%s (%s)", m.User.Mention(), m.User.Username),
			Color:       colorWelcome,
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: m.User.AvatarURL("64")},
			Timestamp:   time.Now().Format(time.RFC3339),
			Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("ID: %s", m.User.ID)},
		})
	}
}

func replaceWelcomeVars(msg string, user *discordgo.User, guildName string, memberCount int) string {
	msg = strings.ReplaceAll(msg, "{user}", user.Mention())
	msg = strings.ReplaceAll(msg, "{username}", user.Username)
	msg = strings.ReplaceAll(msg, "{server}", guildName)
	msg = strings.ReplaceAll(msg, "{memberCount}", fmt.Sprintf("%d", memberCount))
	return msg
}
