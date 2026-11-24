package metaldogs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/production"
)

func TestEnabled_ExtraMap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		extra map[string]string
		on    bool
		bad   bool
	}{
		{name: "nil", on: false},
		{name: "empty", extra: map[string]string{"enabled": ""}, on: false},
		{name: "false", extra: map[string]string{"enabled": "false"}, on: false},
		{name: "true", extra: map[string]string{"enabled": "true"}, on: true},
		{name: "maybe", extra: map[string]string{"enabled": "maybe"}, bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			on, err := Enabled(tc.extra)
			if tc.bad {
				if err == nil || !strings.Contains(err.Error(), "is not a bool") {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || on != tc.on {
				t.Fatalf("on=%v err=%v, want on=%v", on, err, tc.on)
			}
		})
	}
}

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

func TestSettingsFrom_OrderSnapshotFlag(t *testing.T) {
	got, err := SettingsFrom(func(string) string { return "" })
	if err != nil || got.OrderSnapshotEnabled {
		t.Fatalf("default %+v err=%v", got, err)
	}
	got, err = SettingsFrom(func(key string) string {
		if key == "plugins.metaldogs.order_snapshot.enabled" {
			return "true"
		}
		return ""
	})
	if err != nil || !got.OrderSnapshotEnabled {
		t.Fatalf("on %+v err=%v", got, err)
	}
	got, err = SettingsFrom(func(key string) string {
		if key == "plugins.metaldogs.order_snapshot_enabled" {
			return "yes"
		}
		return ""
	})
	if err != nil || !got.OrderSnapshotEnabled {
		t.Fatalf("underscore %+v err=%v", got, err)
	}
	_, err = SettingsFrom(func(key string) string {
		if key == "plugins.metaldogs.order_snapshot.enabled" {
			return "maybe"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "order_snapshot.enabled") || !strings.Contains(err.Error(), "is not a bool") {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsFrom_ThumbnailKeyAndPublicBase(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	got, err := SettingsFrom(func(string) string { return "" })
	if err != nil || got.ThumbnailSigningKey != "" || got.PublicBaseURL != "" {
		t.Fatalf("absent %+v err=%v", got, err)
	}
	got, err = SettingsFrom(func(key string) string {
		switch key {
		case "plugins.metaldogs.thumbnail_signing_key":
			return "  " + secret + "  "
		case "plugins.metaldogs.public_base_url":
			return "https://shop.example/store/"
		default:
			return ""
		}
	})
	if err != nil || got.ThumbnailSigningKey != secret || got.PublicBaseURL != "https://shop.example/store" {
		t.Fatalf("parsed %+v err=%v", got, err)
	}
	_, err = SettingsFrom(func(key string) string {
		if key == "plugins.metaldogs.thumbnail_signing_key" {
			return "short-key"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "thumbnail_signing_key") || !strings.Contains(err.Error(), "32") || strings.Contains(err.Error(), "short-key") {
		t.Fatalf("err = %v", err)
	}
	for _, raw := range []string{"shop.example", "ftp://shop.example", "https://shop.example?x=1", "https://user:pass@shop.example"} {
		_, err = SettingsFrom(func(key string) string {
			if key == "plugins.metaldogs.public_base_url" {
				return raw
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "public_base_url") {
			t.Fatalf("raw %q err = %v", raw, err)
		}
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

func TestSettingsFrom_EmptyEnabledEnvKeepsYAML(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_ENABLED", "")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
`)
	if got := cfg.Plugins.Extra["metaldogs"]["enabled"]; got != "true" {
		t.Fatalf("Extra enabled = %q, want YAML true when env is empty", got)
	}
	got := settingsViaHost(t, cfg)
	if !got.Enabled {
		t.Fatalf("settings = %+v, empty env must keep the plugin enabled", got)
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

func TestSettingsFrom_ProductionSpecAbsent(t *testing.T) {
	got, err := SettingsFrom(func(string) string { return "" })
	if err != nil || got.ProductionSpec != nil {
		t.Fatalf("absent spec = %+v err=%v", got.ProductionSpec, err)
	}
	if got.Provider != "fake" || got.MaxRetries != 3 || got.OrderSnapshotEnabled {
		t.Fatalf("other settings changed: %+v", got)
	}
}

func TestSettingsFrom_ProductionSpecComplete(t *testing.T) {
	got, err := SettingsFrom(productionSpecGet(map[string]string{
		"width_px": "120", "height_px": "80", "dpi": "96",
		"require_transparency": "yes", "color_profile": "SRGB", "upscale_policy": "UpScale",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := &production.Spec{
		WidthPx: 120, HeightPx: 80, DPI: 96,
		RequireTransparency: true,
		ColorProfile:        production.ProfileSRGB,
		UpscalePolicy:       production.PolicyUpscale,
	}
	if got.ProductionSpec == nil || *got.ProductionSpec != *want {
		t.Fatalf("spec = %+v", got.ProductionSpec)
	}
	if got.Provider != "fake" || got.MaxRetries != 3 {
		t.Fatalf("other settings changed: %+v", got)
	}
}

func TestSettingsFrom_ProductionSpecMissingField(t *testing.T) {
	full := map[string]string{
		"width_px": "120", "height_px": "80", "dpi": "96",
		"require_transparency": "false", "color_profile": "srgb", "upscale_policy": "reject",
	}
	for field := range full {
		t.Run(field, func(t *testing.T) {
			vals := map[string]string{}
			for k, v := range full {
				if k != field {
					vals[k] = v
				}
			}
			_, err := SettingsFrom(productionSpecGet(vals))
			if err == nil || !strings.Contains(err.Error(), "production_spec."+field) || !strings.Contains(err.Error(), "is required") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSettingsFrom_ProductionSpecInvalid(t *testing.T) {
	base := map[string]string{
		"width_px": "120", "height_px": "80", "dpi": "96",
		"require_transparency": "false", "color_profile": "srgb", "upscale_policy": "reject",
	}
	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{name: "bad profile", set: map[string]string{"color_profile": "cmyk"}, want: "color_profile"},
		{name: "bad policy", set: map[string]string{"upscale_policy": "crop"}, want: "upscale_policy"},
		{name: "bad bool", set: map[string]string{"require_transparency": "maybe"}, want: "is not a bool"},
		{name: "bad int", set: map[string]string{"width_px": "nope"}, want: "is not an int"},
		{name: "zero", set: map[string]string{"dpi": "0"}, want: "must be > 0"},
		{name: "negative", set: map[string]string{"height_px": "-2"}, want: "must be > 0"},
		{name: "scalar", set: map[string]string{"scalar": "120"}, want: "must be a map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vals := map[string]string{}
			for k, v := range base {
				vals[k] = v
			}
			for k, v := range tc.set {
				if k == "scalar" {
					vals["scalar"] = v
					continue
				}
				vals[k] = v
			}
			_, err := SettingsFrom(func(key string) string {
				if key == "plugins.metaldogs.production_spec" {
					return vals["scalar"]
				}
				const prefix = "plugins.metaldogs.production_spec."
				if strings.HasPrefix(key, prefix) {
					return vals[strings.TrimPrefix(key, prefix)]
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSettingsFrom_ProductionSpecYAMLAndEnv(t *testing.T) {
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	cfg := loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    production_spec:
      width_px: 64
      height_px: 32
      dpi: 72
      require_transparency: false
      color_profile: srgb
      upscale_policy: reject
`)
	got := settingsViaHost(t, cfg)
	if got.ProductionSpec == nil || got.ProductionSpec.WidthPx != 64 || got.ProductionSpec.HeightPx != 32 || got.ProductionSpec.DPI != 72 || got.ProductionSpec.RequireTransparency || got.ProductionSpec.UpscalePolicy != production.PolicyReject {
		t.Fatalf("yaml spec = %+v", got.ProductionSpec)
	}

	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_WIDTH_PX", "48")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_HEIGHT_PX", "24")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_DPI", "120")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_REQUIRE_TRANSPARENCY", "true")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_COLOR_PROFILE", "srgb")
	t.Setenv("SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_UPSCALE_POLICY", "upscale")
	cfg = loadMetalDogsConfig(t, "server:\n  host: 127.0.0.1\n")
	got = settingsViaHost(t, cfg)
	if got.ProductionSpec == nil || got.ProductionSpec.WidthPx != 48 || got.ProductionSpec.HeightPx != 24 || got.ProductionSpec.DPI != 120 || !got.ProductionSpec.RequireTransparency || got.ProductionSpec.UpscalePolicy != production.PolicyUpscale {
		t.Fatalf("env spec = %+v", got.ProductionSpec)
	}
	for _, key := range []string{
		"SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_WIDTH_PX",
		"SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_HEIGHT_PX",
		"SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_DPI",
		"SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_REQUIRE_TRANSPARENCY",
		"SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_COLOR_PROFILE",
		"SHOPANDA_PLUGINS_METALDOGS_PRODUCTION_SPEC_UPSCALE_POLICY",
	} {
		t.Setenv(key, "")
	}

	cfg = loadMetalDogsConfig(t, `
server:
  host: 127.0.0.1
plugins:
  metaldogs:
    enabled: true
    production_spec:
      width_px: 64
      height_px: 32
      require_transparency: false
      color_profile: cmyk
      upscale_policy: reject
`)
	app := &plugin.App{Config: cfg}
	h := pluginsdk.AsHostFor(app, PluginName)
	_, err := SettingsFrom(h.ConfigValue)
	if err == nil || !strings.Contains(err.Error(), "production_spec.dpi") {
		t.Fatalf("missing dpi err = %v", err)
	}
}

func productionSpecGet(vals map[string]string) func(string) string {
	return func(key string) string {
		const prefix = "plugins.metaldogs.production_spec."
		if strings.HasPrefix(key, prefix) {
			return vals[strings.TrimPrefix(key, prefix)]
		}
		return ""
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
