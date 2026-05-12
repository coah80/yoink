package bot

import (
	"database/sql"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Config struct {
	Token     string
	AppID     string
	APIURL    string
	PublicURL string
	BotSecret string
	DBPath    string
}

type Bot struct {
	session *discordgo.Session
	cfg     Config
	api     *apiClient
	db      *sql.DB
	cmdIDs  []string
	status  *statusMonitor
}

func New(cfg Config) (*Bot, error) {
	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, err
	}

	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "bot-data.db"
	}

	db, err := initDB(dbPath)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		session: s,
		cfg:     cfg,
		api:     newAPIClient(cfg.APIURL, cfg.BotSecret),
		db:      db,
	}

	// Register event handlers
	s.AddHandler(b.handleInteraction)
	s.AddHandler(b.handleMessageCreate)
	s.AddHandler(b.handleGuildMemberAdd)
	s.AddHandler(b.handleReactionAdd)
	s.AddHandler(b.handleReactionRemove)

	// Set intents
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuildMessageReactions

	return b, nil
}

func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return err
	}

	log.Printf("Bot logged in as %s", b.session.State.User.Username)

	b.status = newStatusMonitor(b.session)
	b.status.start()

	commands := b.commandDefinitions()
	for _, cmd := range commands {
		created, err := b.session.ApplicationCommandCreate(b.cfg.AppID, "", cmd)
		if err != nil {
			log.Printf("Failed to register command %s: %v", cmd.Name, err)
			continue
		}
		b.cmdIDs = append(b.cmdIDs, created.ID)
		log.Printf("Registered command: /%s", created.Name)
	}

	return nil
}

func (b *Bot) Stop() {
	for _, id := range b.cmdIDs {
		b.session.ApplicationCommandDelete(b.cfg.AppID, "", id)
	}
	if b.db != nil {
		b.db.Close()
	}
	b.session.Close()
}

func (b *Bot) commandDefinitions() []*discordgo.ApplicationCommand {
	commands := []*discordgo.ApplicationCommand{
		// Existing commands
		{
			Name:        "yoink",
			Description: "Download a video from social media",
			IntegrationTypes: &[]discordgo.ApplicationIntegrationType{
				discordgo.ApplicationIntegrationGuildInstall,
				discordgo.ApplicationIntegrationUserInstall,
			},
			Contexts: &[]discordgo.InteractionContextType{
				discordgo.InteractionContextGuild,
				discordgo.InteractionContextBotDM,
				discordgo.InteractionContextPrivateChannel,
			},
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "url",
					Description: "The video URL to download",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "format",
					Description: "Output format",
					Required:    false,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "Video (MP4)", Value: "mp4"},
						{Name: "Audio (MP3)", Value: "mp3"},
						{Name: "GIF", Value: "gif"},
						{Name: "Compressed for Discord", Value: "compressed"},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "resume_from",
					Description: "For playlists, start at this video number",
					Required:    false,
					MinValue:    &[]float64{1}[0],
				},
			},
		},
		{
			Name:        "convert",
			Description: "Convert a media file to another format",
			IntegrationTypes: &[]discordgo.ApplicationIntegrationType{
				discordgo.ApplicationIntegrationGuildInstall,
				discordgo.ApplicationIntegrationUserInstall,
			},
			Contexts: &[]discordgo.InteractionContextType{
				discordgo.InteractionContextGuild,
				discordgo.InteractionContextBotDM,
				discordgo.InteractionContextPrivateChannel,
			},
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionAttachment,
					Name:        "file",
					Description: "The file to convert",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "format",
					Description: "Target format",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "MP4", Value: "mp4"},
						{Name: "WebM", Value: "webm"},
						{Name: "MKV", Value: "mkv"},
						{Name: "MOV", Value: "mov"},
						{Name: "MP3", Value: "mp3"},
						{Name: "M4A", Value: "m4a"},
						{Name: "Opus", Value: "opus"},
						{Name: "WAV", Value: "wav"},
						{Name: "FLAC", Value: "flac"},
					},
				},
			},
		},
		{
			Name:                     "set-status",
			Description:              "Set this channel as the status notification channel",
			DefaultMemberPermissions: &[]int64{discordgo.PermissionManageServer}[0],
			Options:                  []*discordgo.ApplicationCommandOption{},
		},
		{
			Name:        "compress",
			Description: "Compress a video to fit Discord's upload limit",
			IntegrationTypes: &[]discordgo.ApplicationIntegrationType{
				discordgo.ApplicationIntegrationGuildInstall,
				discordgo.ApplicationIntegrationUserInstall,
			},
			Contexts: &[]discordgo.InteractionContextType{
				discordgo.InteractionContextGuild,
				discordgo.InteractionContextBotDM,
				discordgo.InteractionContextPrivateChannel,
			},
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionAttachment,
					Name:        "file",
					Description: "The video to compress",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "target_mb",
					Description: "Target size in MB (default: 25)",
					Required:    false,
					MinValue:    &[]float64{1}[0],
					MaxValue:    500,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "preset",
					Description: "Compression preset",
					Required:    false,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "Fast", Value: "fast"},
						{Name: "Balanced (Recommended)", Value: "balanced"},
						{Name: "Quality", Value: "quality"},
					},
				},
			},
		},
	}

	// Append new command sets
	commands = append(commands, moderationCommands()...)
	commands = append(commands, levelingCommands()...)
	commands = append(commands, welcomeCommands()...)
	commands = append(commands, reactionRoleCommands()...)
	commands = append(commands, setupCommands()...)

	return commands
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleCommand(s, i)
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		if strings.HasPrefix(customID, "setup_") {
			b.handleSetupComponent(s, i)
		}
	}
}

func (b *Bot) handleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	switch data.Name {
	// Existing commands
	case "yoink":
		b.handleYoink(s, i)
	case "convert":
		b.handleConvert(s, i)
	case "compress":
		b.handleCompress(s, i)
	case "set-status":
		b.handleSetStatus(s, i)

	// Moderation
	case "ban":
		b.handleBan(s, i)
	case "kick":
		b.handleKick(s, i)
	case "timeout":
		b.handleTimeout(s, i)
	case "warn":
		b.handleWarn(s, i)
	case "history":
		b.handleHistory(s, i)
	case "modlog":
		b.handleModlog(s, i)
	case "note":
		b.handleNote(s, i)
	case "purge":
		b.handlePurge(s, i)
	case "lock":
		b.handleLock(s, i)
	case "unlock":
		b.handleUnlock(s, i)

	// Leveling
	case "level":
		b.handleLevel(s, i)
	case "leaderboard":
		b.handleLeaderboard(s, i)
	case "setlevel":
		b.handleSetLevel(s, i)
	case "xpsettings":
		b.handleXPSettings(s, i)

	// Welcome & AutoRole
	case "welcome":
		b.handleWelcome(s, i)
	case "autorole":
		b.handleAutoRole(s, i)

	// Reaction Roles
	case "reactionrole":
		b.handleReactionRole(s, i)

	// Setup
	case "setup":
		b.handleSetup(s, i)
	}
}

func (b *Bot) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	b.handleMessageXP(s, m)
}
