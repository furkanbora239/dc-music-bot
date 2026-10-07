package bot

import "github.com/bwmarrin/discordgo"

// Commands defines the list of Discord Slash Commands for the music bot.
var Commands = []*discordgo.ApplicationCommand{
	{
		Name:        "play",
		Description: "Play a song or add it to the queue (YouTube Music / YouTube)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: "Song title, artist, or direct URL",
				Required:    true,
			},
		},
	},
	{
		Name:        "pause",
		Description: "Pause the current song",
	},
	{
		Name:        "resume",
		Description: "Resume playback",
	},
	{
		Name:        "skip",
		Description: "Skip the current song",
	},
	{
		Name:        "stop",
		Description: "Stop playback, clear queue, and leave voice channel",
	},
	{
		Name:        "queue",
		Description: "View upcoming songs in the queue",
	},
	{
		Name:        "nowplaying",
		Description: "Show information about the currently playing song",
	},
	{
		Name:        "loop",
		Description: "Configure loop / repeat mode",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "mode",
				Description: "Choose loop mode",
				Required:    true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{
						Name:  "Off",
						Value: "off",
					},
					{
						Name:  "Track (Repeat current song)",
						Value: "track",
					},
					{
						Name:  "Queue (Repeat entire queue)",
						Value: "queue",
					},
				},
			},
		},
	},
	{
		Name:        "shuffle",
		Description: "Shuffle the upcoming queue randomly",
	},
	{
		Name:        "volume",
		Description: "Adjust playback volume (1 - 100%)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "level",
				Description: "Volume level between 1 and 100",
				Required:    true,
				MinValue:    func(f float64) *float64 { return &f }(1),
				MaxValue:    100,
			},
		},
	},
	{
		Name:        "autoplay",
		Description: "Toggle or configure autoplay mode (auto-plays recommended tracks when queue ends)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "mode",
				Description: "Turn autoplay on or off, or check status",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{
						Name:  "On",
						Value: "on",
					},
					{
						Name:  "Off",
						Value: "off",
					},
					{
						Name:  "Status",
						Value: "status",
					},
				},
			},
		},
	},
	{
		Name:        "help",
		Description: "Show available music bot commands and usage",
	},
}
