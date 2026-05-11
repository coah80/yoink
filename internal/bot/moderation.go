package bot

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// moderationCommands returns the slash command definitions for moderation.
func moderationCommands() []*discordgo.ApplicationCommand {
	dmPerm := false
	banPerm := int64(discordgo.PermissionBanMembers)
	kickPerm := int64(discordgo.PermissionKickMembers)
	modPerm := int64(discordgo.PermissionModerateMembers)
	adminPerm := int64(discordgo.PermissionAdministrator)
	msgPerm := int64(discordgo.PermissionManageMessages)
	chanPerm := int64(discordgo.PermissionManageChannels)

	return []*discordgo.ApplicationCommand{
		{
			Name:                     "ban",
			Description:              "Ban a user from the server",
			DefaultMemberPermissions: &banPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to ban", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason for ban"},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "delete_days", Description: "Days of messages to delete (0-7)", MinValue: &[]float64{0}[0], MaxValue: 7},
			},
		},
		{
			Name:                     "kick",
			Description:              "Kick a user from the server",
			DefaultMemberPermissions: &kickPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to kick", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason for kick"},
			},
		},
		{
			Name:                     "timeout",
			Description:              "Timeout a user",
			DefaultMemberPermissions: &modPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to timeout", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "duration", Description: "Duration (e.g. 1h, 30m, 1d)", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason for timeout"},
			},
		},
		{
			Name:                     "warn",
			Description:              "Warn a user",
			DefaultMemberPermissions: &modPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to warn", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason for warning"},
			},
		},
		{
			Name:                     "history",
			Description:              "View a user's moderation history",
			DefaultMemberPermissions: &modPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to look up", Required: true},
			},
		},
		{
			Name:                     "modlog",
			Description:              "Set or disable the mod log channel",
			DefaultMemberPermissions: &adminPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Channel for mod logs (leave empty to disable)"},
			},
		},
		{
			Name:                     "note",
			Description:              "Add a mod note to a user",
			DefaultMemberPermissions: &modPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to add note to", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "note", Description: "Note content", Required: true},
			},
		},
		{
			Name:                     "purge",
			Description:              "Delete multiple messages from a channel",
			DefaultMemberPermissions: &msgPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "count", Description: "Number of messages (1-100)", Required: true, MinValue: &[]float64{1}[0], MaxValue: 100},
				{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Only delete messages from this user"},
			},
		},
		{
			Name:                     "lock",
			Description:              "Lock a channel (prevent sending messages)",
			DefaultMemberPermissions: &chanPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Channel to lock (default: current)"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason for locking"},
			},
		},
		{
			Name:                     "unlock",
			Description:              "Unlock a channel",
			DefaultMemberPermissions: &chanPerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Channel to unlock (default: current)"},
			},
		},
	}
}

// --- Command Handlers ---

func (b *Bot) handleBan(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	reason := getOptionString(data.Options, "reason")
	deleteDays := int(getOptionInt(data.Options, "delete_days"))

	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}
	if reason == "" {
		reason = "No reason provided"
	}

	guildName := resolveGuildName(s, i.GuildID)
	dmUser(s, user.ID, fmt.Sprintf("You have been **banned** from **%s**.\nReason: %s", guildName, reason))

	err := s.GuildBanCreateWithReason(i.GuildID, user.ID, reason, deleteDays)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Ban Failed", err.Error()))
		return
	}

	b.logModAction(i.GuildID, user.ID, i.Member.User.ID, "ban", reason, "")
	b.sendModLog(s, i.GuildID, "Ban", user, i.Member.User, reason, "")

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "User Banned",
		Description: fmt.Sprintf("%s has been banned.\n**Reason:** %s", user.Mention(), reason),
		Color:       colorError,
	})
}

func (b *Bot) handleKick(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	reason := getOptionString(data.Options, "reason")

	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}
	if reason == "" {
		reason = "No reason provided"
	}

	guildName := resolveGuildName(s, i.GuildID)
	dmUser(s, user.ID, fmt.Sprintf("You have been **kicked** from **%s**.\nReason: %s", guildName, reason))

	err := s.GuildMemberDeleteWithReason(i.GuildID, user.ID, reason)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Kick Failed", err.Error()))
		return
	}

	b.logModAction(i.GuildID, user.ID, i.Member.User.ID, "kick", reason, "")
	b.sendModLog(s, i.GuildID, "Kick", user, i.Member.User, reason, "")

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "User Kicked",
		Description: fmt.Sprintf("%s has been kicked.\n**Reason:** %s", user.Mention(), reason),
		Color:       colorMod,
	})
}

func (b *Bot) handleTimeout(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	durationStr := getOptionString(data.Options, "duration")
	reason := getOptionString(data.Options, "reason")

	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}
	if reason == "" {
		reason = "No reason provided"
	}

	dur, err := parseDuration(durationStr)
	if err != nil {
		respondEphemeral(s, i, fmt.Sprintf("Invalid duration: %v", err))
		return
	}

	maxTimeout := 28 * 24 * time.Hour
	if dur > maxTimeout {
		respondEphemeral(s, i, "Timeout cannot exceed 28 days.")
		return
	}

	until := time.Now().Add(dur)
	_, err = s.GuildMemberEdit(i.GuildID, user.ID, &discordgo.GuildMemberParams{
		CommunicationDisabledUntil: &until,
	})
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Timeout Failed", err.Error()))
		return
	}

	b.logModAction(i.GuildID, user.ID, i.Member.User.ID, "timeout", reason, formatDuration(dur))
	b.sendModLog(s, i.GuildID, "Timeout", user, i.Member.User, reason, formatDuration(dur))

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "User Timed Out",
		Description: fmt.Sprintf("%s has been timed out for **%s**.\n**Reason:** %s", user.Mention(), formatDuration(dur), reason),
		Color:       colorMod,
	})
}

func (b *Bot) handleWarn(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	reason := getOptionString(data.Options, "reason")

	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}
	if reason == "" {
		reason = "No reason provided"
	}

	// Insert warning
	now := time.Now().Unix()
	_, err := b.db.Exec(`INSERT INTO warnings (guild_id, user_id, moderator_id, reason, created_at) VALUES (?, ?, ?, ?, ?)`,
		i.GuildID, user.ID, i.Member.User.ID, reason, now)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Warning Failed", err.Error()))
		return
	}

	// Count warnings for auto-escalation
	warnCount := 1
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM warnings WHERE guild_id = ? AND user_id = ?`, i.GuildID, user.ID).Scan(&warnCount); err != nil {
		log.Printf("[Warn] Failed to count warnings: %v", err)
	}

	b.logModAction(i.GuildID, user.ID, i.Member.User.ID, "warn", reason, "")
	b.sendModLog(s, i.GuildID, "Warning", user, i.Member.User, reason, "")

	guildName := resolveGuildName(s, i.GuildID)
	dmUser(s, user.ID, fmt.Sprintf("You have been **warned** in **%s**.\nReason: %s\nThis is warning #%d.", guildName, reason, warnCount))

	desc := fmt.Sprintf("%s has been warned. (Warning #%d)\n**Reason:** %s", user.Mention(), warnCount, reason)

	// Auto-escalation
	if warnCount >= 5 {
		dur := 24 * time.Hour
		until := time.Now().Add(dur)
		if _, err := s.GuildMemberEdit(i.GuildID, user.ID, &discordgo.GuildMemberParams{
			CommunicationDisabledUntil: &until,
		}); err == nil {
			desc += "\n\n**Auto-escalation:** 24h timeout (5+ warnings)"
			b.logModAction(i.GuildID, user.ID, i.Member.User.ID, "timeout (auto)", "5+ warnings", "24h")
		}
	} else if warnCount >= 3 {
		dur := 1 * time.Hour
		until := time.Now().Add(dur)
		if _, err := s.GuildMemberEdit(i.GuildID, user.ID, &discordgo.GuildMemberParams{
			CommunicationDisabledUntil: &until,
		}); err == nil {
			desc += "\n\n**Auto-escalation:** 1h timeout (3+ warnings)"
			b.logModAction(i.GuildID, user.ID, i.Member.User.ID, "timeout (auto)", "3+ warnings", "1h")
		}
	}

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "User Warned",
		Description: desc,
		Color:       colorMod,
	})
}

func (b *Bot) handleHistory(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}

	// Get warnings
	rows, err := b.db.Query(`SELECT reason, moderator_id, created_at FROM warnings WHERE guild_id = ? AND user_id = ? ORDER BY created_at DESC LIMIT 10`,
		i.GuildID, user.ID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}
	defer rows.Close()

	var warnings []string
	for rows.Next() {
		var reason, modID string
		var createdAt int64
		if err := rows.Scan(&reason, &modID, &createdAt); err != nil {
			log.Printf("[History] scan error: %v", err)
			continue
		}
		warnings = append(warnings, fmt.Sprintf("<t:%d:R> by <@%s>: %s", createdAt, modID, reason))
	}

	// Get mod actions
	actionRows, err := b.db.Query(`SELECT action, reason, duration, moderator_id, created_at FROM mod_actions WHERE guild_id = ? AND user_id = ? ORDER BY created_at DESC LIMIT 10`,
		i.GuildID, user.ID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}
	defer actionRows.Close()

	var actions []string
	for actionRows.Next() {
		var action, reason, duration, modID string
		var createdAt int64
		if err := actionRows.Scan(&action, &reason, &duration, &modID, &createdAt); err != nil {
			log.Printf("[History] scan error: %v", err)
			continue
		}
		line := fmt.Sprintf("<t:%d:R> **%s** by <@%s>", createdAt, action, modID)
		if duration != "" {
			line += fmt.Sprintf(" (%s)", duration)
		}
		if reason != "" {
			line += ": " + reason
		}
		actions = append(actions, line)
	}

	// Get notes
	noteRows, err := b.db.Query(`SELECT note, moderator_id, created_at FROM mod_notes WHERE guild_id = ? AND user_id = ? ORDER BY created_at DESC LIMIT 5`,
		i.GuildID, user.ID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}
	defer noteRows.Close()

	var notes []string
	for noteRows.Next() {
		var note, modID string
		var createdAt int64
		if err := noteRows.Scan(&note, &modID, &createdAt); err != nil {
			log.Printf("[History] scan error: %v", err)
			continue
		}
		notes = append(notes, fmt.Sprintf("<t:%d:R> by <@%s>: %s", createdAt, modID, note))
	}

	fields := []*discordgo.MessageEmbedField{}

	if len(warnings) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  fmt.Sprintf("Warnings (%d)", len(warnings)),
			Value: strings.Join(warnings, "\n"),
		})
	}
	if len(actions) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  fmt.Sprintf("Mod Actions (%d)", len(actions)),
			Value: strings.Join(actions, "\n"),
		})
	}
	if len(notes) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  fmt.Sprintf("Notes (%d)", len(notes)),
			Value: strings.Join(notes, "\n"),
		})
	}

	desc := ""
	if len(fields) == 0 {
		desc = "No moderation history for this user."
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("History for %s", user.Username),
		Description: desc,
		Color:       colorMod,
		Fields:      fields,
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: user.AvatarURL("64")},
	})
}

func (b *Bot) handleModlog(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	ch := getOptionChannel(data.Options, "channel", data.Resolved)

	gs, err := getGuildSettings(b.db, i.GuildID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	if ch == nil {
		if _, err := b.db.Exec(`UPDATE guild_settings SET mod_log_channel = '' WHERE guild_id = ?`, gs.GuildID); err != nil {
			respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
			return
		}
		respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
			Title:       "Mod Log Disabled",
			Description: "Mod log channel has been disabled.",
			Color:       colorMod,
		})
		return
	}

	if _, err := b.db.Exec(`UPDATE guild_settings SET mod_log_channel = ? WHERE guild_id = ?`, ch.ID, gs.GuildID); err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}
	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Mod Log Set",
		Description: fmt.Sprintf("Mod actions will be logged to <#%s>.", ch.ID),
		Color:       colorSuccess,
	})
}

func (b *Bot) handleNote(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	user := getOptionUser(data.Options, "user", data.Resolved)
	note := getOptionString(data.Options, "note")

	if user == nil {
		respondEphemeral(s, i, "Could not resolve user.")
		return
	}

	now := time.Now().Unix()
	_, err := b.db.Exec(`INSERT INTO mod_notes (guild_id, user_id, moderator_id, note, created_at) VALUES (?, ?, ?, ?, ?)`,
		i.GuildID, user.ID, i.Member.User.ID, note, now)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Note Added",
		Description: fmt.Sprintf("Note added to %s: %s", user.Mention(), note),
		Color:       colorSuccess,
	})
}

func (b *Bot) handlePurge(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	count := int(getOptionInt(data.Options, "count"))
	filterUser := getOptionUser(data.Options, "user", data.Resolved)

	if count < 1 || count > 100 {
		respondEphemeral(s, i, "Count must be between 1 and 100.")
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	messages, err := s.ChannelMessages(i.ChannelID, count+1, "", "", "")
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{errorEmbed("Purge Failed", err.Error())},
		})
		return
	}

	var toDelete []string
	twoWeeksAgo := time.Now().Add(-14 * 24 * time.Hour)
	for _, msg := range messages {
		if msg.ID == "" {
			continue
		}
		ts, _ := discordgo.SnowflakeTimestamp(msg.ID)
		if ts.Before(twoWeeksAgo) {
			continue
		}
		if filterUser != nil && (msg.Author == nil || msg.Author.ID != filterUser.ID) {
			continue
		}
		toDelete = append(toDelete, msg.ID)
		if len(toDelete) >= count {
			break
		}
	}

	if len(toDelete) == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{errorEmbed("No Messages", "No matching messages found to delete.")},
		})
		return
	}

	if len(toDelete) == 1 {
		err = s.ChannelMessageDelete(i.ChannelID, toDelete[0])
	} else {
		err = s.ChannelMessagesBulkDelete(i.ChannelID, toDelete)
	}
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{errorEmbed("Purge Failed", err.Error())},
		})
		return
	}

	desc := fmt.Sprintf("Deleted **%d** messages.", len(toDelete))
	if filterUser != nil {
		desc += fmt.Sprintf(" (from %s)", filterUser.Mention())
	}

	b.logModAction(i.GuildID, "", i.Member.User.ID, "purge", desc, "")

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{{
			Title:       "Messages Purged",
			Description: desc,
			Color:       colorSuccess,
		}},
	})
}

func (b *Bot) handleLock(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	ch := getOptionChannel(data.Options, "channel", data.Resolved)
	reason := getOptionString(data.Options, "reason")

	channelID := i.ChannelID
	if ch != nil {
		channelID = ch.ID
	}

	err := s.ChannelPermissionSet(channelID, i.GuildID, discordgo.PermissionOverwriteTypeRole, 0, discordgo.PermissionSendMessages)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Lock Failed", err.Error()))
		return
	}

	desc := fmt.Sprintf("<#%s> has been locked.", channelID)
	if reason != "" {
		desc += fmt.Sprintf("\n**Reason:** %s", reason)
	}

	b.logModAction(i.GuildID, channelID, i.Member.User.ID, "lock", reason, "")
	b.sendModLog(s, i.GuildID, "Channel Lock", nil, i.Member.User, reason, "")

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Channel Locked",
		Description: desc,
		Color:       colorMod,
	})
}

func (b *Bot) handleUnlock(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	ch := getOptionChannel(data.Options, "channel", data.Resolved)

	channelID := i.ChannelID
	if ch != nil {
		channelID = ch.ID
	}

	err := s.ChannelPermissionDelete(channelID, i.GuildID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Unlock Failed", err.Error()))
		return
	}

	b.logModAction(i.GuildID, channelID, i.Member.User.ID, "unlock", "", "")

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Channel Unlocked",
		Description: fmt.Sprintf("<#%s> has been unlocked.", channelID),
		Color:       colorSuccess,
	})
}

// --- Helpers ---

func (b *Bot) logModAction(guildID, userID, modID, action, reason, duration string) {
	now := time.Now().Unix()
	_, err := b.db.Exec(`INSERT INTO mod_actions (guild_id, user_id, moderator_id, action, reason, duration, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		guildID, userID, modID, action, reason, duration, now)
	if err != nil {
		log.Printf("[Mod] Failed to log action: %v", err)
	}
}

func (b *Bot) sendModLog(s *discordgo.Session, guildID, action string, target, moderator *discordgo.User, reason, duration string) {
	gs, err := getGuildSettings(b.db, guildID)
	if err != nil || gs.ModLogChannel == "" {
		return
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Moderator", Value: moderator.Mention(), Inline: true},
	}
	if target != nil {
		fields = append([]*discordgo.MessageEmbedField{
			{Name: "User", Value: fmt.Sprintf("%s (%s)", target.Mention(), target.ID), Inline: true},
		}, fields...)
	}
	if reason != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Reason", Value: reason})
	}
	if duration != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Duration", Value: duration, Inline: true})
	}

	s.ChannelMessageSendEmbed(gs.ModLogChannel, &discordgo.MessageEmbed{
		Title:     action,
		Color:     colorMod,
		Fields:    fields,
		Timestamp: time.Now().Format(time.RFC3339),
		Footer:    &discordgo.MessageEmbedFooter{Text: "Mod Log"},
	})
}

func dmUser(s *discordgo.Session, userID, message string) {
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		return // silently fail if DMs closed
	}
	s.ChannelMessageSend(ch.ID, message)
}

// resolveGuildName returns the guild name, falling back to the guild ID.
func resolveGuildName(s *discordgo.Session, guildID string) string {
	if guild, err := s.Guild(guildID); err == nil {
		return guild.Name
	}
	return guildID
}

// ensureGuildSettings is used by setup and other commands.
func ensureGuildSettings(db *sql.DB, guildID string) {
	db.Exec(`INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)`, guildID)
}
