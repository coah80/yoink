package bot

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

func reactionRoleCommands() []*discordgo.ApplicationCommand {
	dmPerm := false
	rolePerm := int64(discordgo.PermissionManageRoles)

	return []*discordgo.ApplicationCommand{
		{
			Name:                     "reactionrole",
			Description:              "Manage reaction roles",
			DefaultMemberPermissions: &rolePerm,
			DMPermission:             &dmPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "add",
					Description: "Add a reaction role",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "message_id", Description: "Message ID", Required: true},
						{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Channel containing the message", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "emoji", Description: "Emoji to react with", Required: true},
						{Type: discordgo.ApplicationCommandOptionRole, Name: "role", Description: "Role to assign", Required: true},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "remove",
					Description: "Remove a reaction role",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "message_id", Description: "Message ID", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "emoji", Description: "Emoji to remove", Required: true},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "list",
					Description: "List reaction roles for a message",
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "message_id", Description: "Message ID", Required: true},
					},
				},
			},
		},
	}
}

// --- Command Handlers ---

func (b *Bot) handleReactionRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		respondEphemeral(s, i, "Please specify a subcommand.")
		return
	}

	sub := data.Options[0]

	switch sub.Name {
	case "add":
		b.handleReactionRoleAdd(s, i, sub, data.Resolved)
	case "remove":
		b.handleReactionRoleRemove(s, i, sub)
	case "list":
		b.handleReactionRoleList(s, i, sub)
	}
}

func (b *Bot) handleReactionRoleAdd(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption, resolved *discordgo.ApplicationCommandInteractionDataResolved) {
	messageID := getOptionString(sub.Options, "message_id")
	ch := getOptionChannel(sub.Options, "channel", resolved)
	emoji := getOptionString(sub.Options, "emoji")
	role := getOptionRole(sub.Options, "role", resolved)

	if ch == nil || role == nil {
		respondEphemeral(s, i, "Please provide all required options.")
		return
	}

	// Verify the message exists
	_, err := s.ChannelMessage(ch.ID, messageID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", "Could not find that message. Make sure the message ID and channel are correct."))
		return
	}

	// Add the bot's reaction to the message
	err = s.MessageReactionAdd(ch.ID, messageID, emoji)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", fmt.Sprintf("Could not add reaction: %v. Make sure the emoji is valid.", err)))
		return
	}

	// Save to database
	_, err = b.db.Exec(`INSERT OR REPLACE INTO reaction_roles (message_id, emoji, role_id) VALUES (?, ?, ?)`,
		messageID, emoji, role.ID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Reaction Role Added",
		Description: fmt.Sprintf("React with %s on [message](<https://discord.com/channels/%s/%s/%s>) to get <@&%s>.", emoji, i.GuildID, ch.ID, messageID, role.ID),
		Color:       colorSuccess,
	})
}

func (b *Bot) handleReactionRoleRemove(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	messageID := getOptionString(sub.Options, "message_id")
	emoji := getOptionString(sub.Options, "emoji")

	result, err := b.db.Exec(`DELETE FROM reaction_roles WHERE message_id = ? AND emoji = ?`, messageID, emoji)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		respondEphemeral(s, i, "No reaction role found for that message and emoji.")
		return
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Reaction Role Removed",
		Description: fmt.Sprintf("Removed %s reaction role from message %s.", emoji, messageID),
		Color:       colorSuccess,
	})
}

func (b *Bot) handleReactionRoleList(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	messageID := getOptionString(sub.Options, "message_id")

	rows, err := b.db.Query(`SELECT emoji, role_id FROM reaction_roles WHERE message_id = ?`, messageID)
	if err != nil {
		respondEphemeralEmbed(s, i, errorEmbed("Error", err.Error()))
		return
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var emoji, roleID string
		rows.Scan(&emoji, &roleID)
		lines = append(lines, fmt.Sprintf("%s → <@&%s>", emoji, roleID))
	}

	desc := "No reaction roles configured for this message."
	if len(lines) > 0 {
		desc = ""
		for _, l := range lines {
			desc += l + "\n"
		}
	}

	respondEphemeralEmbed(s, i, &discordgo.MessageEmbed{
		Title:       "Reaction Roles",
		Description: desc,
		Color:       colorLevel,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Message ID: %s", messageID)},
	})
}

// --- Event Handlers ---

func (b *Bot) handleReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r.UserID == s.State.User.ID {
		return // ignore bot's own reactions
	}

	emoji := r.Emoji.Name
	if r.Emoji.ID != "" {
		emoji = r.Emoji.Name + ":" + r.Emoji.ID
	}

	var roleID string
	err := b.db.QueryRow(`SELECT role_id FROM reaction_roles WHERE message_id = ? AND emoji = ?`,
		r.MessageID, emoji).Scan(&roleID)
	if err != nil {
		// Also try with just the name for custom emoji
		if r.Emoji.ID != "" {
			err = b.db.QueryRow(`SELECT role_id FROM reaction_roles WHERE message_id = ? AND emoji = ?`,
				r.MessageID, r.Emoji.Name).Scan(&roleID)
		}
		if err != nil {
			return // no reaction role configured
		}
	}

	if err := s.GuildMemberRoleAdd(r.GuildID, r.UserID, roleID); err != nil {
		log.Printf("[ReactionRole] Failed to add role: %v", err)
	}
}

func (b *Bot) handleReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	if r.UserID == s.State.User.ID {
		return
	}

	emoji := r.Emoji.Name
	if r.Emoji.ID != "" {
		emoji = r.Emoji.Name + ":" + r.Emoji.ID
	}

	var roleID string
	err := b.db.QueryRow(`SELECT role_id FROM reaction_roles WHERE message_id = ? AND emoji = ?`,
		r.MessageID, emoji).Scan(&roleID)
	if err != nil {
		if r.Emoji.ID != "" {
			err = b.db.QueryRow(`SELECT role_id FROM reaction_roles WHERE message_id = ? AND emoji = ?`,
				r.MessageID, r.Emoji.Name).Scan(&roleID)
		}
		if err != nil {
			return
		}
	}

	if err := s.GuildMemberRoleRemove(r.GuildID, r.UserID, roleID); err != nil {
		log.Printf("[ReactionRole] Failed to remove role: %v", err)
	}
}
