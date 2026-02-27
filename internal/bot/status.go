package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	statusCheckInterval = 60 * time.Second
	statusConfigFile    = "status-config.json"
	statusPageURL       = "https://status.yoink.tools"
	healthCheckURL      = "https://yoink.tools/health"
)

type statusConfig struct {
	GuildChannels map[string]string `json:"guildChannels"` // guildID -> channelID
}

type siteStatus struct {
	up   bool
	code int
}

type statusMonitor struct {
	session  *discordgo.Session
	config   statusConfig
	mu       sync.RWMutex
	lastUp   *bool
	client   *http.Client
}

func newStatusMonitor(s *discordgo.Session) *statusMonitor {
	m := &statusMonitor{
		session: s,
		config:  statusConfig{GuildChannels: make(map[string]string)},
		client:  &http.Client{Timeout: 10 * time.Second},
	}
	m.loadConfig()
	return m
}

func (m *statusMonitor) loadConfig() {
	data, err := os.ReadFile(statusConfigFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &m.config)
	if m.config.GuildChannels == nil {
		m.config.GuildChannels = make(map[string]string)
	}
}

func (m *statusMonitor) saveConfig() error {
	m.mu.RLock()
	data, err := json.MarshalIndent(m.config, "", "  ")
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(statusConfigFile, data, 0644)
}

func (m *statusMonitor) setChannel(guildID, channelID string) error {
	m.mu.Lock()
	m.config.GuildChannels[guildID] = channelID
	m.mu.Unlock()
	return m.saveConfig()
}

func (m *statusMonitor) checkHealth() siteStatus {
	resp, err := m.client.Get(healthCheckURL)
	if err != nil {
		return siteStatus{up: false, code: 0}
	}
	defer resp.Body.Close()
	return siteStatus{up: resp.StatusCode == 200, code: resp.StatusCode}
}

func (m *statusMonitor) start() {
	go func() {
		// initial check after short delay
		time.Sleep(5 * time.Second)
		m.tick()

		ticker := time.NewTicker(statusCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			m.tick()
		}
	}()
	log.Println("[Status] Monitor started, checking every 60s")
}

func (m *statusMonitor) tick() {
	status := m.checkHealth()

	if m.lastUp == nil {
		// first check, just record state
		m.lastUp = &status.up
		state := "UP"
		if !status.up {
			state = "DOWN"
		}
		log.Printf("[Status] Initial state: %s (HTTP %d)", state, status.code)
		return
	}

	wasUp := *m.lastUp
	if status.up == wasUp {
		return
	}

	// state changed
	m.lastUp = &status.up
	log.Printf("[Status] State changed: up=%v -> up=%v", wasUp, status.up)
	m.broadcast(status)
}

func (m *statusMonitor) broadcast(status siteStatus) {
	m.mu.RLock()
	channels := make(map[string]string, len(m.config.GuildChannels))
	for k, v := range m.config.GuildChannels {
		channels[k] = v
	}
	m.mu.RUnlock()

	if len(channels) == 0 {
		return
	}

	embed := m.statusEmbed(status)

	for guildID, channelID := range channels {
		_, err := m.session.ChannelMessageSendEmbed(channelID, embed)
		if err != nil {
			log.Printf("[Status] Failed to send to guild %s channel %s: %v", guildID, channelID, err)
		}
	}
}

func (b *Bot) handleSetStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "This command can only be used in a server.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	channelID := i.ChannelID
	if err := b.status.setChannel(i.GuildID, channelID); err != nil {
		log.Printf("[Status] Failed to save config: %v", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Failed to save status channel config.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "Status channel set",
					Description: fmt.Sprintf("Status updates will be posted to <#%s>.\n\nYou'll be notified when yoink.tools goes down or comes back up.\n\n[View status page](%s)", channelID, statusPageURL),
					Color:       colorSuccess,
					Footer:      &discordgo.MessageEmbedFooter{Text: "yoink status"},
				},
			},
		},
	})
}

func (m *statusMonitor) statusEmbed(status siteStatus) *discordgo.MessageEmbed {
	if status.up {
		return &discordgo.MessageEmbed{
			Title:       "yoink.tools is back online",
			Description: fmt.Sprintf("All systems operational.\n\n[View status page](%s)", statusPageURL),
			Color:       colorSuccess,
			Timestamp:   time.Now().Format(time.RFC3339),
			Footer:      &discordgo.MessageEmbedFooter{Text: "yoink status"},
		}
	}

	desc := "yoink.tools appears to be down."
	if status.code > 0 {
		desc += fmt.Sprintf(" (HTTP %d)", status.code)
	}
	desc += fmt.Sprintf("\n\n[View status page](%s)", statusPageURL)

	return &discordgo.MessageEmbed{
		Title:       "yoink.tools is down",
		Description: desc,
		Color:       colorError,
		Timestamp:   time.Now().Format(time.RFC3339),
		Footer:      &discordgo.MessageEmbedFooter{Text: "yoink status"},
	}
}
