package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

const (
	defaultGitHubAPIURL    = "https://api.github.com"
	defaultGitHubBranch    = "main"
	defaultListenAddr      = ":8080"
	defaultNoteDir         = "Inbox/Quick Capture"
	defaultAttachmentBytes = 25 * 1024 * 1024
)

type Config struct {
	GitHubToken       string
	GitHubOwner       string
	GitHubRepo        string
	GitHubBranch      string
	GitHubAPIURL      string
	WebhookToken      string
	ListenAddr        string
	DebugListenAddr   string
	NoteDir           string
	MaxAttachmentSize int64
}

func Load() (Config, error) {
	cfg := Config{
		GitHubToken:       strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		GitHubOwner:       strings.TrimSpace(os.Getenv("GITHUB_OWNER")),
		GitHubRepo:        strings.TrimSpace(os.Getenv("GITHUB_REPO")),
		GitHubBranch:      envOrDefault("GITHUB_BRANCH", defaultGitHubBranch),
		GitHubAPIURL:      envOrDefault("GITHUB_API_URL", defaultGitHubAPIURL),
		WebhookToken:      strings.TrimSpace(os.Getenv("WEBHOOK_TOKEN")),
		ListenAddr:        envOrDefault("LISTEN_ADDR", defaultListenAddr),
		DebugListenAddr:   strings.TrimSpace(os.Getenv("DEBUG_LISTEN_ADDR")),
		NoteDir:           envOrDefault("NOTE_DIR", defaultNoteDir),
		MaxAttachmentSize: defaultAttachmentBytes,
	}

	if err := cfg.loadMaxAttachmentSize(); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (cfg *Config) loadMaxAttachmentSize() error {
	raw := strings.TrimSpace(os.Getenv("MAX_ATTACHMENT_BYTES"))
	if raw == "" {
		return nil
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("parse MAX_ATTACHMENT_BYTES: %w", err)
	}
	if value <= 0 {
		return errors.New("MAX_ATTACHMENT_BYTES must be greater than zero")
	}

	cfg.MaxAttachmentSize = value
	return nil
}

func (cfg *Config) validate() error {
	var missing []string
	required := map[string]string{
		"GITHUB_TOKEN":  cfg.GitHubToken,
		"GITHUB_OWNER":  cfg.GitHubOwner,
		"GITHUB_REPO":   cfg.GitHubRepo,
		"WEBHOOK_TOKEN": cfg.WebhookToken,
	}

	for name, value := range required {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	noteDir, err := cleanRepoDir(cfg.NoteDir)
	if err != nil {
		return fmt.Errorf("invalid NOTE_DIR: %w", err)
	}

	cfg.NoteDir = noteDir
	return nil
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	return value
}

func cleanRepoDir(value string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", errors.New("must be a relative repository path")
	}

	return cleaned, nil
}
