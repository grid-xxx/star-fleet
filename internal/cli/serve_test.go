package cli

import (
	"testing"
)

func TestResolveEnvFlag_FlagWins(t *testing.T) {
	t.Parallel()
	got := resolveEnvFlag("flag-val", "TEST_RESOLVE_KEY_UNUSED")
	if got != "flag-val" {
		t.Errorf("got %q, want %q", got, "flag-val")
	}
}

func TestResolveEnvFlag_EnvFallback(t *testing.T) {
	t.Setenv("TEST_RESOLVE_KEY", "env-val")
	got := resolveEnvFlag("", "TEST_RESOLVE_KEY")
	if got != "env-val" {
		t.Errorf("got %q, want %q", got, "env-val")
	}
}

func TestResolveEnvFlag_BothEmpty(t *testing.T) {
	t.Setenv("TEST_RESOLVE_KEY2", "")
	got := resolveEnvFlag("", "TEST_RESOLVE_KEY2")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestServeCmdFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flag     string
		defValue string
	}{
		{"port", "port", "8888"},
		{"webhook-secret", "webhook-secret", ""},
		{"workdir", "workdir", "/data/fleet-workdirs"},
		{"app-id", "app-id", ""},
		{"app-private-key", "app-private-key", ""},
		{"label", "label", "fleet"},
		{"bot-user", "bot-user", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := serveCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not found", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("flag %q default = %q, want %q", tt.flag, f.DefValue, tt.defValue)
			}
		})
	}
}

func TestServeCmdFlags_RepoRemoved(t *testing.T) {
	t.Parallel()

	f := serveCmd.Flags().Lookup("repo")
	if f != nil {
		t.Error("--repo flag should have been removed from serve command")
	}
}

func TestServeCmdRegistered(t *testing.T) {
	t.Parallel()

	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "serve" {
			found = true
			break
		}
	}
	if !found {
		t.Error("serve command not registered on rootCmd")
	}
}

func TestResolveServeConfig_MissingSecret(t *testing.T) {
	t.Setenv("FLEET_WEBHOOK_SECRET", "")

	_, err := resolveServeConfig("", "", "")
	if err == nil {
		t.Fatal("expected error when webhook secret is missing")
	}
}

func TestResolveServeConfig_MissingAppID(t *testing.T) {
	t.Setenv("FLEET_APP_ID", "")

	_, err := resolveServeConfig("test-secret", "", "")
	if err == nil {
		t.Fatal("expected error when app ID is missing")
	}
}

func TestResolveServeConfig_MissingPrivateKey(t *testing.T) {
	t.Setenv("FLEET_APP_PRIVATE_KEY_PATH", "")

	_, err := resolveServeConfig("test-secret", "12345", "")
	if err == nil {
		t.Fatal("expected error when private key is missing")
	}
}

func TestResolveServeConfig_InvalidAppID(t *testing.T) {
	_, err := resolveServeConfig("test-secret", "not-a-number", "/some/path.pem")
	if err == nil {
		t.Fatal("expected error for invalid app ID")
	}
}
