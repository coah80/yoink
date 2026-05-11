package bot

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	setupTimeout = 5 * time.Minute
	colorSetup   = 0x9B59B6 // Purple
)

type setupState struct {
	mu        sync.Mutex
	guildID   string
	userID    string
	step      int // 0=modlog, 1=welcome, 2=autorole, 3=levelroles
	messageID string
	createdAt time.Time
}

var (
	setupStates   sync.Map // interactionID -> *setupState
	setupStepName = []string{"Mod Log", "Welcome", "Auto Role", "Level Roles"}
)

// Default level roles created by setup wizard
var defaultLevelRoles = []struct {
	Name  string
	Level int
}{
	{"Newcomer", 5},
	{"Regular", 10},
	{"Active", 25},
	{"Veteran", 50},
	{"Legend", 100},
}

func setupCommands() []*discordgo.ApplicationCommand {
	dmPerm := false
	adminPerm := int64(discordgo.PermissionAdministrator)
	return []*discordgo.ApplicationCommand{
		{
			Name:                     "setup",
			Description:              "Interactive setup wizard for the bot",
			DefaultMemberPermissions: &adminPerm,
			DMPermission:             &dmPerm,
		},
	}
}

func (b *Bot) handleSetup(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !requireGuild(s, i) {
		return
	}
	ensureGuildSettings(b.db, i.GuildID)

	state := &setupState{
		guildID:   i.GuildID,
		userID:    i.Member.User.ID,
		step:      0,
		createdAt: time.Now(),
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{setupStepEmbed(state)},
			Components: setupStepComponents(state),
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	})

	setupStates.Store(i.ID, state)

	// Cleanup after timeout
	go func() {
		time.Sleep(setupTimeout)
		setupStates.Delete(i.ID)
	}()
}

func (b *Bot) handleSetupComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Member == nil {
		return
	}
	customID := i.MessageComponentData().CustomID

	// Find the setup state for this user/guild
	var state *setupState
	var stateKey interface{}
	setupStates.Range(func(key, value interface{}) bool {
		st := value.(*setupState)
		if st.guildID == i.GuildID && st.userID == i.Member.User.ID {
			state = st
			stateKey = key
			return false
		}
		return true
	})

	if state == nil || time.Since(state.createdAt) > setupTimeout {
		respondEphemeral(s, i, "Setup session expired. Run `/setup` again.")
		return
	}

	// Lock the state to prevent concurrent modification from rapid button clicks
	state.mu.Lock()
	defer state.mu.Unlock()

	switch customID {
	case "setup_select":
		b.setupSelectExisting(s, i, state)
	case "setup_create":
		b.setupCreateForMe(s, i, state, stateKey)
	case "setup_skip":
		b.setupAdvance(s, i, state, stateKey)
	case "setup_back":
		if state.step > 0 {
			state.step--
		}
		b.setupUpdateMessage(s, i, state)
	case "setup_finish":
		setupStates.Delete(stateKey)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{{
					Title:       "Setup Complete",
					Description: "Your bot is configured! Use individual commands to make further adjustments.",
					Color:       colorSuccess,
				}},
				Components: []discordgo.MessageComponent{},
			},
		})
	case "setup_channel_select", "setup_role_select":
		b.setupHandleSelect(s, i, state, stateKey)
	}
}

func (b *Bot) setupSelectExisting(s *discordgo.Session, i *discordgo.InteractionCreate, state *setupState) {
	var components []discordgo.MessageComponent

	switch state.step {
	case 0: // Mod Log - pick channel
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    "setup_channel_select",
					Placeholder: "Select a channel for mod logs",
					MenuType:    discordgo.ChannelSelectMenu,
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildText,
					},
				},
			}},
		}
	case 1: // Welcome - pick channel
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    "setup_channel_select",
					Placeholder: "Select a welcome channel",
					MenuType:    discordgo.ChannelSelectMenu,
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildText,
					},
				},
			}},
		}
	case 2: // Auto Role - pick role
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    "setup_role_select",
					Placeholder: "Select a role for new members",
					MenuType:    discordgo.RoleSelectMenu,
				},
			}},
		}
	case 3: // Level Roles - skip selection, just create
		b.setupCreateForMe(s, i, state, nil)
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{setupStepEmbed(state)},
			Components: components,
		},
	})
}

func (b *Bot) setupHandleSelect(s *discordgo.Session, i *discordgo.InteractionCreate, state *setupState, stateKey interface{}) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}

	selectedID := values[0]

	switch state.step {
	case 0: // Mod Log channel selected
		b.db.Exec(`UPDATE guild_settings SET mod_log_channel = ? WHERE guild_id = ?`, selectedID, state.guildID)
	case 1: // Welcome channel selected
		defaultMsg := "Welcome {user} to **{server}**! You're member #{memberCount}."
		b.db.Exec(`UPDATE guild_settings SET welcome_channel = ?, welcome_message = ? WHERE guild_id = ?`,
			selectedID, defaultMsg, state.guildID)
	case 2: // Auto Role selected
		b.db.Exec(`UPDATE guild_settings SET auto_role = ? WHERE guild_id = ?`, selectedID, state.guildID)
	}

	b.setupAdvance(s, i, state, stateKey)
}

func (b *Bot) setupCreateForMe(s *discordgo.Session, i *discordgo.InteractionCreate, state *setupState, stateKey interface{}) {
	// If stateKey not passed, find it
	if stateKey == nil {
		setupStates.Range(func(key, value interface{}) bool {
			st := value.(*setupState)
			if st.guildID == i.GuildID && st.userID == i.Member.User.ID {
				stateKey = key
				return false
			}
			return true
		})
	}

	switch state.step {
	case 0: // Create #mod-log
		ch, err := s.GuildChannelCreate(state.guildID, "mod-log", discordgo.ChannelTypeGuildText)
		if err != nil {
			log.Printf("[Setup] Failed to create mod-log channel: %v", err)
		} else {
			b.db.Exec(`UPDATE guild_settings SET mod_log_channel = ? WHERE guild_id = ?`, ch.ID, state.guildID)
		}

	case 1: // Create #welcome
		ch, err := s.GuildChannelCreate(state.guildID, "welcome", discordgo.ChannelTypeGuildText)
		if err != nil {
			log.Printf("[Setup] Failed to create welcome channel: %v", err)
		} else {
			defaultMsg := "Welcome {user} to **{server}**! You're member #{memberCount}."
			b.db.Exec(`UPDATE guild_settings SET welcome_channel = ?, welcome_message = ? WHERE guild_id = ?`,
				ch.ID, defaultMsg, state.guildID)
		}

	case 2: // Create Member role
		role, err := s.GuildRoleCreate(state.guildID, &discordgo.RoleParams{
			Name: "Member",
		})
		if err != nil {
			log.Printf("[Setup] Failed to create Member role: %v", err)
		} else {
			b.db.Exec(`UPDATE guild_settings SET auto_role = ? WHERE guild_id = ?`, role.ID, state.guildID)
		}

	case 3: // Create level roles
		for _, lr := range defaultLevelRoles {
			role, err := s.GuildRoleCreate(state.guildID, &discordgo.RoleParams{
				Name: lr.Name,
			})
			if err != nil {
				log.Printf("[Setup] Failed to create role %s: %v", lr.Name, err)
				continue
			}
			b.db.Exec(`INSERT OR REPLACE INTO level_roles (guild_id, level, role_id) VALUES (?, ?, ?)`,
				state.guildID, lr.Level, role.ID)
		}
	}

	b.setupAdvance(s, i, state, stateKey)
}

func (b *Bot) setupAdvance(s *discordgo.Session, i *discordgo.InteractionCreate, state *setupState, stateKey interface{}) {
	state.step++

	if state.step >= len(setupStepName) {
		if stateKey != nil {
			setupStates.Delete(stateKey)
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{{
					Title:       "Setup Complete",
					Description: "Your bot is configured! Use individual commands to make further adjustments.\n\nConfigured:\n- `/modlog` — Mod log channel\n- `/welcome` — Welcome messages\n- `/autorole` — Auto-role\n- `/xpsettings` — Level roles & XP",
					Color:       colorSuccess,
				}},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	b.setupUpdateMessage(s, i, state)
}

func (b *Bot) setupUpdateMessage(s *discordgo.Session, i *discordgo.InteractionCreate, state *setupState) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{setupStepEmbed(state)},
			Components: setupStepComponents(state),
		},
	})
}

func setupStepEmbed(state *setupState) *discordgo.MessageEmbed {
	stepDescriptions := []string{
		"**Step 1: Mod Log**\nSet up a channel where moderation actions (bans, kicks, warns, etc.) will be logged.",
		"**Step 2: Welcome**\nSet up a welcome channel for new member greetings.",
		"**Step 3: Auto Role**\nSet a role that will be automatically assigned to new members.",
		"**Step 4: Level Roles**\nCreate roles that are automatically assigned as members level up.\nDefaults: Newcomer (5), Regular (10), Active (25), Veteran (50), Legend (100)",
	}

	desc := stepDescriptions[state.step]
	desc += fmt.Sprintf("\n\n*Step %d of %d*", state.step+1, len(setupStepName))

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Bot Setup — %s", setupStepName[state.step]),
		Description: desc,
		Color:       colorSetup,
	}
}

func setupStepComponents(state *setupState) []discordgo.MessageComponent {
	buttons := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    "Select existing",
			Style:    discordgo.PrimaryButton,
			CustomID: "setup_select",
		},
		discordgo.Button{
			Label:    "Create for me",
			Style:    discordgo.SecondaryButton,
			CustomID: "setup_create",
		},
		discordgo.Button{
			Label:    "Skip",
			Style:    discordgo.SecondaryButton,
			CustomID: "setup_skip",
		},
	}

	if state.step > 0 {
		buttons = append([]discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Back",
				Style:    discordgo.SecondaryButton,
				CustomID: "setup_back",
			},
		}, buttons...)
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: buttons},
	}
}
