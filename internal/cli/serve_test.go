package cli

import (
	"os"
	"testing"
)

func TestResolveWebhookSecret_Flag(t *testing.T) {
	t.Parallel()
	got := resolveWebhookSecret("my-flag-secret")
	if got != "my-flag-secret" {
		t.Errorf("got %q, want %q", got, "my-flag-secret")
	}
}

func TestResolveWebhookSecret_Env(t *testing.T) {
	t.Setenv("FLEET_WEBHOOK_SECRET", "my-env-secret")
	got := resolveWebhookSecret("")
	if got != "my-env-secret" {
		t.Errorf("got %q, want %q", got, "my-env-secret")
	}
}

func TestResolveWebhookSecret_FlagOverridesEnv(t *testing.T) {
	t.Setenv("FLEET_WEBHOOK_SECRET", "env-val")
	got := resolveWebhookSecret("flag-val")
	if got != "flag-val" {
		t.Errorf("got %q, want %q", got, "flag-val")
	}
}

func TestResolveWebhookSecret_Empty(t *testing.T) {
	os.Unsetenv("FLEET_WEBHOOK_SECRET")
	got := resolveWebhookSecret("")
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
		{"repo", "repo", "."},
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

func TestRunServe_MissingSecret(t *testing.T) {
	os.Unsetenv("FLEET_WEBHOOK_SECRET")

	old := serveWebhookSecret
	serveWebhookSecret = ""
	defer func() { serveWebhookSecret = old }()

	err := runServe(serveCmd, nil)
	if err == nil {
		t.Fatal("expected error when webhook secret is missing")
	}
}
