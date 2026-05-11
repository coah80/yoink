package bot

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	log.Printf("[DB] Initialized database at %s", path)
	return db, nil
}

func createTables(db *sql.DB) error {
	tables := `
	CREATE TABLE IF NOT EXISTS guild_settings (
		guild_id TEXT PRIMARY KEY,
		mod_log_channel TEXT DEFAULT '',
		welcome_channel TEXT DEFAULT '',
		welcome_message TEXT DEFAULT '',
		auto_role TEXT DEFAULT '',
		member_role TEXT DEFAULT '',
		xp_per_message INTEGER DEFAULT 15,
		xp_cooldown INTEGER DEFAULT 60,
		level_announce_channel TEXT DEFAULT '',
		level_announce_dm INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS user_levels (
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		xp INTEGER DEFAULT 0,
		level INTEGER DEFAULT 0,
		messages INTEGER DEFAULT 0,
		last_xp_time INTEGER DEFAULT 0,
		PRIMARY KEY (guild_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS level_roles (
		guild_id TEXT NOT NULL,
		level INTEGER NOT NULL,
		role_id TEXT NOT NULL,
		PRIMARY KEY (guild_id, level)
	);

	CREATE TABLE IF NOT EXISTS warnings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		moderator_id TEXT NOT NULL,
		reason TEXT DEFAULT '',
		created_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS mod_notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		moderator_id TEXT NOT NULL,
		note TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS mod_actions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		moderator_id TEXT NOT NULL,
		action TEXT NOT NULL,
		reason TEXT DEFAULT '',
		duration TEXT DEFAULT '',
		created_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS reaction_roles (
		message_id TEXT NOT NULL,
		emoji TEXT NOT NULL,
		role_id TEXT NOT NULL,
		PRIMARY KEY (message_id, emoji)
	);
	`
	_, err := db.Exec(tables)
	return err
}

// getGuildSettings retrieves or creates guild settings, returning defaults if none exist.
func getGuildSettings(db *sql.DB, guildID string) (*guildSettings, error) {
	gs := &guildSettings{GuildID: guildID}
	var levelAnnounceDM int
	err := db.QueryRow(`SELECT guild_id, mod_log_channel, welcome_channel, welcome_message,
		auto_role, member_role, xp_per_message, xp_cooldown, level_announce_channel, level_announce_dm
		FROM guild_settings WHERE guild_id = ?`, guildID).Scan(
		&gs.GuildID, &gs.ModLogChannel, &gs.WelcomeChannel, &gs.WelcomeMessage,
		&gs.AutoRole, &gs.MemberRole, &gs.XPPerMessage, &gs.XPCooldown,
		&gs.LevelAnnounceChannel, &levelAnnounceDM,
	)
	if err == sql.ErrNoRows {
		// Insert defaults and return them
		_, err = db.Exec(`INSERT INTO guild_settings (guild_id) VALUES (?)`, guildID)
		if err != nil {
			return nil, err
		}
		gs.XPPerMessage = 15
		gs.XPCooldown = 60
		return gs, nil
	}
	gs.LevelAnnounceDM = levelAnnounceDM != 0
	return gs, err
}

type guildSettings struct {
	GuildID              string
	ModLogChannel        string
	WelcomeChannel       string
	WelcomeMessage       string
	AutoRole             string
	MemberRole           string
	XPPerMessage         int
	XPCooldown           int
	LevelAnnounceChannel string
	LevelAnnounceDM      bool
}
