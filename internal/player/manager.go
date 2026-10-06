package player

import (
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/user/dc-music-bot/internal/audio"
)

// Manager coordinates all active Guild players across Discord servers.
type Manager struct {
	players map[string]*Player
	mu      sync.RWMutex
}

// NewManager creates a player manager.
func NewManager() *Manager {
	return &Manager{
		players: make(map[string]*Player),
	}
}

// Get returns the player for a guild if one exists.
func (m *Manager) Get(guildID string) *Player {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.players[guildID]
}

// GetOrCreate returns an existing player or creates and starts a new one.
func (m *Manager) GetOrCreate(
	guildID, voiceChannelID, textChannelID string,
	session *discordgo.Session,
	resolver *audio.Resolver,
	defaultVolume int,
	idleTimeout time.Duration,
) *Player {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, exists := m.players[guildID]; exists {
		if voiceChannelID != "" {
			p.SetVoiceChannel(voiceChannelID)
		}
		if textChannelID != "" {
			p.SetTextChannel(textChannelID)
		}
		return p
	}

	p := NewPlayer(
		guildID,
		voiceChannelID,
		textChannelID,
		session,
		resolver,
		defaultVolume,
		idleTimeout,
		func(gID string) {
			m.Remove(gID)
		},
	)
	m.players[guildID] = p
	return p
}

// Remove removes and stops a guild player.
func (m *Manager) Remove(guildID string) {
	m.mu.Lock()
	p, exists := m.players[guildID]
	if exists {
		delete(m.players, guildID)
	}
	m.mu.Unlock()

	if exists && p != nil {
		p.Close()
	}
}

// Shutdown cleanly stops all active players.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	allPlayers := make([]*Player, 0, len(m.players))
	for _, p := range m.players {
		allPlayers = append(allPlayers, p)
	}
	m.players = make(map[string]*Player)
	m.mu.Unlock()

	for _, p := range allPlayers {
		p.Close()
	}
}
