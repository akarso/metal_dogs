package metaldogs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestSettingsFrom_ProductionEnvNames(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_ENABLED", "yes")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_PROVIDER", "fake")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_RETRIES", "4")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_ATTEMPTS_PER_DESIGN", "5")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS", "12")

	cfg := loadMetalDogsConfig(t, "server:\n  host: 127.0.0.1\n")
	extra := cfg.Plugins.Extra["metaldogs"]
	if extra["generation.provider"] != "fake" || extra["generation_provider"] != "fake" {
		t.Fatalf("alias keys = %#v, want both generation.provider and generation_provider", extra)
	}
	got := settingsViaHost(t, cfg)
	if !got.Enabled || got.Provider != "fake" || got.MaxRetries != 4 || got.MaxAttemptsPerDesign != 5 || got.CooldownSeconds != 12 {
		t.Fatalf("settings = %+v", got)
	}
}

func TestSettingsFrom_DefaultsAndZeroCounts(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_ENABLED", "true")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_RETRIES", "0")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_ATTEMPTS_PER_DESIGN", "-2")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS", "0")

	cfg := loadMetalDogsConfig(t, "server:\n  host: 127.0.0.1\n")
	got := settingsViaHost(t, cfg)
	if got.Provider != "fake" || got.MaxRetries != 3 || got.MaxAttemptsPerDesign != 3 || got.CooldownSeconds != 0 {
		t.Fatalf("settings = %+v", got)
	}
}

func TestSettingsFrom_BadIntAndBool(t *testing.T) {
	_, err := SettingsFrom(func(key string) string {
		switch key {
		case "plugins.metaldogs.enabled":
			return "true"
		case "plugins.metaldogs.generation_max_retries":
			return "nope"
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "is not an int") {
		t.Fatalf("err = %v", err)
	}

	_, err = SettingsFrom(func(key string) string {
		if key == "plugins.metaldogs.enabled" {
			return "maybe"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "is not a bool") {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsFrom_ScalarGenerationKey(t *testing.T) {
	_, err := SettingsFrom(func(key string) string {
		if key == "plugins.metaldogs.generation" {
			return "fake"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "must be a map") || strings.Contains(err.Error(), "cannot store nested") {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsFrom_DottedYAMLAlias(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    generation.provider: fake
    generation.max_retries: 2
    generation.max_attempts_per_design: 6
    generation.cooldown_seconds: 9
`)
	got := settingsViaHost(t, cfg)
	if !got.Enabled || got.Provider != "fake" || got.MaxRetries != 2 || got.MaxAttemptsPerDesign != 6 || got.CooldownSeconds != 9 {
		t.Fatalf("settings = %+v", got)
	}
}

func TestSettingsFrom_NestedGenerationYAMLMatchesTyped(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    generation:
      provider: fake
      max_retries: 4
      max_attempts_per_design: 6
      cooldown_seconds: 9
`)
	got := settingsViaHost(t, cfg)
	if !got.Enabled || got.Provider != "fake" || got.MaxRetries != 4 || got.MaxAttemptsPerDesign != 6 || got.CooldownSeconds != 9 {
		t.Fatalf("settings = %+v", got)
	}
	if raw := cfg.Plugins.Extra["metaldogs"]["generation"]; raw != "" {
		t.Fatalf("generation Extra key = %q, want dotted keys only", raw)
	}
}

func TestSettingsFrom_GenerationWithoutCooldownDefaultsTo30(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    generation:
      provider: fake
      max_retries: 4
`)
	got := settingsViaHost(t, cfg)
	if !got.Enabled || got.Provider != "fake" || got.MaxRetries != 4 || got.MaxAttemptsPerDesign != 3 || got.CooldownSeconds != 30 {
		t.Fatalf("settings = %+v, want cooldown 30 when generation map omits cooldown_seconds", got)
	}
}

func TestSettingsFrom_EnvOverridesNestedYAMLMultiWordKeys(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_RETRIES", "4")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_MAX_ATTEMPTS_PER_DESIGN", "7")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_GENERATION_COOLDOWN_SECONDS", "12")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    generation:
      provider: fake
      max_retries: 2
      max_attempts_per_design: 6
      cooldown_seconds: 9
`)
	extra := cfg.Plugins.Extra["metaldogs"]
	if extra["generation.max_retries"] != "2" || extra["generation_max_retries"] != "4" || extra["generation.max.retries"] != "4" {
		t.Fatalf("max_retries aliases = max_retries:%q max.retries:%q underscore:%q",
			extra["generation.max_retries"], extra["generation.max.retries"], extra["generation_max_retries"])
	}
	if extra["generation.cooldown_seconds"] != "9" || extra["generation_cooldown_seconds"] != "12" || extra["generation.cooldown.seconds"] != "12" {
		t.Fatalf("cooldown aliases = cooldown_seconds:%q cooldown.seconds:%q underscore:%q",
			extra["generation.cooldown_seconds"], extra["generation.cooldown.seconds"], extra["generation_cooldown_seconds"])
	}
	got := settingsViaHost(t, cfg)
	if !got.Enabled || got.Provider != "fake" || got.MaxRetries != 4 || got.MaxAttemptsPerDesign != 7 || got.CooldownSeconds != 12 {
		t.Fatalf("settings = %+v, want env over nested YAML", got)
	}
}

func TestSettingsFrom_KnownDifference_EmptyEnabledEnvOverwritesYAML(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_ENABLED", "")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
`)
	if got := cfg.Plugins.Extra["metaldogs"]["enabled"]; got != "" {
		t.Fatalf("Extra enabled = %q, want empty env overwrite", got)
	}
	got := settingsViaHost(t, cfg)
	if got.Enabled {
		t.Fatalf("settings = %+v, empty env is stored and ParseEnabled treats it as off", got)
	}
}

func TestSettingsFrom_NestedGenerationBadInt(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    generation:
      provider: fake
      max_retries: nope
`)
	app := &plugin.App{Config: cfg}
	h := pluginsdk.AsHostFor(app, PluginName)
	_, err := SettingsFrom(h.ConfigValue)
	if err == nil || !strings.Contains(err.Error(), "is not an int") {
		t.Fatalf("err = %v", err)
	}
}

func loadMetalDogsConfig(t *testing.T, yaml string) *config.Config {
	t.Helper()
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_AUTH_JWT_SECRET", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("SHOPANDA_MEDIA_LOCAL_SIGN_SECRET", "unit-test-media-local-sign-secret-32b")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(path))
	res, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return res.Config
}

func settingsViaHost(t *testing.T, cfg *config.Config) Settings {
	t.Helper()
	app := &plugin.App{Config: cfg}
	h := pluginsdk.AsHostFor(app, PluginName)
	got, err := SettingsFrom(h.ConfigValue)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
