package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadLogClientErrors(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LOG_CLIENT_ERRORS", "true")

	cfg, err := Load()

	require.NoError(t, err)
	require.True(t, cfg.LogClientErrors)
}

func TestLoadRejectsInvalidLogClientErrors(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LOG_CLIENT_ERRORS", "sometimes")

	_, err := Load()

	require.ErrorContains(t, err, "parse LOG_CLIENT_ERRORS")
}

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("GITHUB_TOKEN", "token")
	t.Setenv("GITHUB_OWNER", "owner")
	t.Setenv("GITHUB_REPO", "repo")
	t.Setenv("WEBHOOK_TOKEN", "secret")
}
