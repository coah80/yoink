package bot

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const colorLevel = 0x3498DB // Blue

// XP formula: level = floor(0.1 * sqrt(xp))
func xpToLevel(xp int) int {
	return int(math.Floor(0.1 * math.Sqrt(float64(xp))))
}

// xpForLevel returns the minimum XP needed for a given level.
func xpForLevel(level int) int {
	// level = 0.1 * sqrt(xp)  =>  xp = (level / 0.1)^2 = (10*level)^2
	return (10 * level) * (10 * level)
}

func levelingCommands() []*discordgo.ApplicationCommand {
	dmPerm := false
	adminPerm := int64(discordgo.PermissionAdministrator)

	return []*discordgo.ApplicationCommand{
		{
			Name:         "level",
			Description:  "Check your or another user's level",
			DMPermission: &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to check (default: yourself)"},
			},
		},
		{
			Name:         "leaderboard",
			Description:  "View the server XP leaderboard",
			DMPermission: &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "page", Description: "Page number (default: 1)", MinValue: &[]float64{1}[0]},
			},
		},
		{
			Name:                     "setlevel",
			Description:              "Set a user's level",
			DefaultMemberPermissions: &adminPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to set", Required: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "level", Description: "Level to set", Required: true, MinValue: &[]float64{0}[0]},
			},
		},
		{
			Name:                     "xpsettings",
			Description:              "Configure XP settings",
			DefaultMemberPermissions: &adminPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "view",
					Description: "View current XP settings",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "xpmessage",
					Description: "Set XP per message",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionInteger, Name: "amount", Description: "XP per message (1-100)", Required: true, MinValue: &[]float64{1}[0], MaxValue: 100},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "cooldown",
					Description: "Set XP cooldown in seconds",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionInteger, Name: "seconds", Description: "Cooldown in seconds (0-300)", Required: true, MinValue: &[]float64{0}[0], MaxValue: 300},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "announce",
					Description: "Set level-up announcement channel",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Channel (leave empty to use current channel)"},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "dm",
					Description: "Toggle level-up DM notifications",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionBoolean, Name: "enabled", Description: "Enable DM notifications", Required: true},
					},
				},
			},
		},
	}
}

// --- Command Handlers ---

func (b *Bot) handleLevel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	if user == nil {
		user = i.Member.User
	}

	var xp, level, messages int
	err := b.db.QueryRow(`SELECT xp, level, messages FROM user_levels WHERE guild_id = ? AND user_id = ?`,
		i.GuildID, user.ID).Scan(&xp, &level, &messages)
	if err != nil {
		xp, level, messages = 0, 0, 0
	}

	nextLevel := level + 1
	currentLevelXP := xpForLevel(level)
	nextLevelXP := xpForLevel(nextLevel)
	progress := float64(0)
	if nextLevelXP > currentLevelXP {
		progress = float64(xp-currentLevelXP) / float64(nextLevelXP-currentLevelXP) * 100
	}
	if progress < 0 {
		progress = 0
	}

	bar := levelProgressBar(progress)

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Level %d", level),
		Description: fmt.Sprintf("**%s**\n\n%s %.0f%%\n\n**XP:** %d / %d\n**Messages:** %d",
			user.Username, bar, progress, xp, nextLevelXP, messages),
		Color:     colorLevel,
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: user.AvatarURL("128")},
	})
}

func (b *Bot) handleLeaderboard(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	page := int(getOptionInt(data.Options, "page"))
	if page < 1 {
		page = 1
	}

	perPage := 10
	offset := (page - 1) * perPage

	rows, err := b.db.Query(`SELECT user_id, xp, level, messages FROM user_levels
		WHERE guild_id = ? ORDER BY xp DESC LIMIT ? OFFSET ?`,
		i.GuildID, perPage, offset)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}
	defer rows.Close()

	var lines []string
	rank := offset
	for rows.Next() {
		rank++
		var userID string
		var xp, level, messages int
		if err := rows.Scan(&userID, &xp, &level, &messages); err != nil {
			log.Printf("[Leaderboard] scan error: %v", err)
			continue
		}

		medal := fmt.Sprintf("`#%d`", rank)
		switch rank {
		case 1:
			medal = "**#1**"
		case 2:
			medal = "**#2**"
		case 3:
			medal = "**#3**"
		}

		lines = append(lines, fmt.Sprintf("%s <@%s> — Level %d (%d XP)", medal, userID, level, xp))
	}

	desc := "No users on the leaderboard yet."
	if len(lines) > 0 {
		desc = strings.Join(lines, "\n")
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Leaderboard",
		Description: desc,
		Color:       colorLevel,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Page %d", page)},
	})
}

func (b *Bot) handleSetLevel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	level := int(getOptionInt(data.Options, "level"))

	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}

	xp := xpForLevel(level)

	_, err := b.db.Exec(`INSERT INTO user_levels (guild_id, user_id, xp, level, messages, last_xp_time)
		VALUES (?, ?, ?, ?, 0, 0)
		ON CONFLICT(guild_id, user_id) DO UPDATE SET xp = ?, level = ?`,
		i.GuildID, user.ID, xp, level, xp, level)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Level Set",
		Description: fmt.Sprintf("%s has been set to **Level %d** (%d XP).", user.Mention(), level, xp),
		Color:       colorSuccess,
	})
}

func (b *Bot) handleXPSettings(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	case "view":
		announceStr := "Current channel"
		if gs.LevelAnnounceChannel != "" {
			announceStr = fmt.Sprintf("<#%s>", gs.LevelAnnounceChannel)
		}
		dmStr := "Disabled"
		if gs.LevelAnnounceDM {
			dmStr = "Enabled"
		}

		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title: "XP Settings",
			Fields: []*discordgo.MessageEmbedField{
				{Name: "XP Per Message", Value: fmt.Sprintf("%d", gs.XPPerMessage), Inline: true},
				{Name: "Cooldown", Value: fmt.Sprintf("%ds", gs.XPCooldown), Inline: true},
				{Name: "Announce Channel", Value: announceStr, Inline: true},
				{Name: "DM Notifications", Value: dmStr, Inline: true},
			},
			Color: colorLevel,
		})

	case "xpmessage":
		amount := int(getOptionInt(sub.Options, "amount"))
		if _, err := b.db.Exec(`UPDATE guild_settings SET xp_per_message = ? WHERE guild_id = ?`, amount, gs.GuildID); err != nil {
			respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
			return
		}
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "XP Per Message Updated",
			Description: fmt.Sprintf("XP per message set to **%d**.", amount),
			Color:       colorSuccess,
		})

	case "cooldown":
		seconds := int(getOptionInt(sub.Options, "seconds"))
		if _, err := b.db.Exec(`UPDATE guild_settings SET xp_cooldown = ? WHERE guild_id = ?`, seconds, gs.GuildID); err != nil {
			respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
			return
		}
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "XP Cooldown Updated",
			Description: fmt.Sprintf("XP cooldown set to **%d seconds**.", seconds),
			Color:       colorSuccess,
		})

	case "announce":
		ch := getOptionChannel(sub.Options, "channel", data.Resolved)
		if ch != nil {
			if _, err := b.db.Exec(`UPDATE guild_settings SET level_announce_channel = ? WHERE guild_id = ?`, ch.ID, gs.GuildID); err != nil {
				respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
				return
			}
			respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
				Title:       "Announce Channel Set",
				Description: fmt.Sprintf("Level-up announcements will be posted to <#%s>.", ch.ID),
				Color:       colorSuccess,
			})
		} else {
			if _, err := b.db.Exec(`UPDATE guild_settings SET level_announce_channel = '' WHERE guild_id = ?`, gs.GuildID); err != nil {
				respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
				return
			}
			respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
				Title:       "Announce Channel Reset",
				Description: "Level-up announcements will be posted in the current channel.",
				Color:       colorSuccess,
			})
		}

	case "dm":
		enabled := false
		for _, opt := range sub.Options {
			if opt.Name == "enabled" {
				enabled = opt.BoolValue()
			}
		}
		val := 0
		if enabled {
			val = 1
		}
		if _, err := b.db.Exec(`UPDATE guild_settings SET level_announce_dm = ? WHERE guild_id = ?`, val, gs.GuildID); err != nil {
			respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
			return
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "DM Notifications Updated",
			Description: fmt.Sprintf("Level-up DM notifications **%s**.", state),
			Color:       colorSuccess,
		})
	}
}

// --- Message Handler for XP ---

func (b *Bot) handleMessageXP(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Skip bots, DMs, short messages
	if m.Author.Bot || m.GuildID == "" || len(m.Content) < 3 {
		return
	}

	gs, err := getGuildSettings(b.db, m.GuildID)
	if err != nil {
		return
	}

	now := time.Now().Unix()

	// Check cooldown
	var lastXPTime int64
	err = b.db.QueryRow(`SELECT last_xp_time FROM user_levels WHERE guild_id = ? AND user_id = ?`,
		m.GuildID, m.Author.ID).Scan(&lastXPTime)
	if err == nil && now-lastXPTime < int64(gs.XPCooldown) {
		// Still on cooldown, but always increment messages
		b.db.Exec(`UPDATE user_levels SET messages = messages + 1 WHERE guild_id = ? AND user_id = ?`,
			m.GuildID, m.Author.ID)
		return
	}

	// Award XP atomically using a transaction
	xpGain := gs.XPPerMessage + rand.Intn(10)

	tx, err := b.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	// Upsert user level record
	_, err = tx.Exec(`INSERT INTO user_levels (guild_id, user_id, xp, level, messages, last_xp_time)
		VALUES (?, ?, ?, 0, 1, ?)
		ON CONFLICT(guild_id, user_id) DO UPDATE SET
			xp = xp + ?, messages = messages + 1, last_xp_time = ?`,
		m.GuildID, m.Author.ID, xpGain, now, xpGain, now)
	if err != nil {
		return
	}

	// Check for level up within same transaction
	var xp, oldLevel int
	err = tx.QueryRow(`SELECT xp, level FROM user_levels WHERE guild_id = ? AND user_id = ?`,
		m.GuildID, m.Author.ID).Scan(&xp, &oldLevel)
	if err != nil {
		return
	}

	newLevel := xpToLevel(xp)
	if newLevel > oldLevel {
		tx.Exec(`UPDATE user_levels SET level = ? WHERE guild_id = ? AND user_id = ?`,
			newLevel, m.GuildID, m.Author.ID)
	}

	if err := tx.Commit(); err != nil {
		return
	}

	// Announce and assign roles outside the transaction
	if newLevel > oldLevel {
		b.assignLevelRoles(s, m.GuildID, m.Author.ID, newLevel)
		b.announceLevelUp(s, m, gs, newLevel)
	}
}

func (b *Bot) assignLevelRoles(s *discordgo.Session, guildID, userID string, level int) {
	rows, err := b.db.Query(`SELECT role_id FROM level_roles WHERE guild_id = ? AND level <= ? ORDER BY level`,
		guildID, level)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			continue
		}
		s.GuildMemberRoleAdd(guildID, userID, roleID)
	}
}

func (b *Bot) announceLevelUp(s *discordgo.Session, m *discordgo.MessageCreate, gs *guildSettings, level int) {
	msg := fmt.Sprintf("**%s** just reached **Level %d**!", m.Author.Username, level)

	if gs.LevelAnnounceDM {
		guildName := resolveGuildName(s, m.GuildID)
		dmUser(s, m.Author.ID, fmt.Sprintf("You just reached **Level %d** in **%s**!", level, guildName))
	}

	channelID := m.ChannelID
	if gs.LevelAnnounceChannel != "" {
		channelID = gs.LevelAnnounceChannel
	}

	s.ChannelMessageSendEmbed(channelID, &discordgo.MessageEmbed{
		Description: msg,
		Color:       colorLevel,
	})
}

func levelProgressBar(percent float64) string {
	filled := int(percent / 5)
	if filled > 20 {
		filled = 20
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("\u2593", filled) + strings.Repeat("\u2591", 20-filled)
}
