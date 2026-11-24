package metaldogs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/production"
)

const metalDogsSlug = "metaldogs"

// Settings is the effective plugins.metaldogs configuration.
type Settings struct {
	Enabled              bool
	Provider             string
	MaxRetries           int
	MaxAttemptsPerDesign int
	CooldownSeconds      int
	OrderSnapshotEnabled bool
	// ProductionSpec is nil when plugins.metaldogs.production_spec is absent.
	ProductionSpec *production.Spec
}

// Enabled reports whether plugins.metaldogs.enabled is on.
// extra is the plugins.metaldogs map (nil or a missing value is off).
// Values other than 1/true/yes and 0/false/no are errors.
func Enabled(extra map[string]string) (bool, error) {
	if extra == nil {
		return false, nil
	}
	return ParseEnabled(extra["enabled"])
}

// ParseEnabled interprets the Extra string for plugins.metaldogs.enabled.
func ParseEnabled(raw string) (bool, error) {
	return parseSettingBool(raw, "enabled")
}

func parseSettingBool(raw, name string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "no":
		return false, nil
	case "1", "true", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s %q is not a bool", name, raw)
	}
}

// SettingsFrom reads plugins.metaldogs.* via Host.ConfigValue; bad ints/bools are errors.
func SettingsFrom(get func(string) string) (Settings, error) {
	if get == nil {
		get = func(string) string { return "" }
	}
	if nested := strings.TrimSpace(get(metalDogsKey("generation"))); nested != "" {
		return Settings{}, fmt.Errorf("metaldogs plugin: plugins.metaldogs.generation must be a map (provider, max_retries, …) or flat generation_* keys, got %q", nested)
	}
	enabled, err := ParseEnabled(get(metalDogsKey("enabled")))
	if err != nil {
		return Settings{}, err
	}
	provider := strings.TrimSpace(preferUnderscore(get, "generation.provider", "generation_provider"))
	if provider == "" {
		provider = "fake"
	}
	if provider != "fake" {
		return Settings{}, fmt.Errorf("metaldogs plugin: generation.provider %q not supported in C4 (use fake)", provider)
	}
	maxRetries, err := parseCount(get, "generation.max_retries", "generation_max_retries", 3)
	if err != nil {
		return Settings{}, err
	}
	maxAttempts, err := parseCount(get, "generation.max_attempts_per_design", "generation_max_attempts_per_design", 3)
	if err != nil {
		return Settings{}, err
	}
	cooldown, err := parseCooldown(get)
	if err != nil {
		return Settings{}, err
	}
	orderSnapshot, err := parseSettingBool(preferUnderscore(get, "order_snapshot.enabled", "order_snapshot_enabled"), "order_snapshot.enabled")
	if err != nil {
		return Settings{}, err
	}
	prod, err := parseProductionSpec(get)
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		Enabled:              enabled,
		Provider:             provider,
		MaxRetries:           maxRetries,
		MaxAttemptsPerDesign: maxAttempts,
		CooldownSeconds:      cooldown,
		OrderSnapshotEnabled: orderSnapshot,
		ProductionSpec:       prod,
	}, nil
}

var productionSpecFields = []string{
	"width_px",
	"height_px",
	"dpi",
	"require_transparency",
	"color_profile",
	"upscale_policy",
}

func parseProductionSpec(get func(string) string) (*production.Spec, error) {
	scalar := strings.TrimSpace(get(metalDogsKey("production_spec")))
	raw := make(map[string]string, len(productionSpecFields))
	any := scalar != ""
	for _, field := range productionSpecFields {
		dotted := "production_spec." + field
		v := strings.TrimSpace(preferUnderscore(get, dotted, "production_spec_"+field))
		raw[field] = v
		if v != "" {
			any = true
		}
	}
	if !any {
		return nil, nil
	}
	if scalar != "" {
		return nil, fmt.Errorf("metaldogs plugin: plugins.metaldogs.production_spec must be a map (width_px, height_px, …), got %q", scalar)
	}
	width, err := parseRequiredPositiveInt(raw["width_px"], "production_spec.width_px")
	if err != nil {
		return nil, err
	}
	height, err := parseRequiredPositiveInt(raw["height_px"], "production_spec.height_px")
	if err != nil {
		return nil, err
	}
	dpi, err := parseRequiredPositiveInt(raw["dpi"], "production_spec.dpi")
	if err != nil {
		return nil, err
	}
	transparent, err := parseRequiredBool(raw["require_transparency"], "production_spec.require_transparency")
	if err != nil {
		return nil, err
	}
	profile := strings.ToLower(raw["color_profile"])
	if profile == "" {
		return nil, fmt.Errorf("metaldogs plugin: plugins.metaldogs.production_spec.color_profile is required")
	}
	if profile != production.ProfileSRGB {
		return nil, fmt.Errorf("metaldogs plugin: plugins.metaldogs.production_spec.color_profile %q is not supported (srgb only)", raw["color_profile"])
	}
	policy := strings.ToLower(raw["upscale_policy"])
	switch policy {
	case "":
		return nil, fmt.Errorf("metaldogs plugin: plugins.metaldogs.production_spec.upscale_policy is required")
	case production.PolicyReject, production.PolicyUpscale:
	default:
		return nil, fmt.Errorf("metaldogs plugin: plugins.metaldogs.production_spec.upscale_policy %q is not supported (reject|upscale)", raw["upscale_policy"])
	}
	return &production.Spec{
		WidthPx:             width,
		HeightPx:            height,
		DPI:                 dpi,
		RequireTransparency: transparent,
		ColorProfile:        profile,
		UpscalePolicy:       policy,
	}, nil
}

func parseRequiredPositiveInt(raw, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s is required", name)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s %q is not an int", name, raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s must be > 0", name)
	}
	return n, nil
}

func parseRequiredBool(raw, name string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return false, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s is required", name)
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s %q is not a bool", name, raw)
	}
}

func metalDogsKey(key string) string {
	return "plugins." + metalDogsSlug + "." + key
}

// Env does not overwrite mixed YAML keys (generation.max_retries); see CR-21 Shopanda gaps.
func preferUnderscore(get func(string) string, dotted, underscore string) string {
	if v := get(metalDogsKey(underscore)); strings.TrimSpace(v) != "" {
		return v
	}
	return get(metalDogsKey(dotted))
}

func parseCount(get func(string) string, dotted, underscore string, def int) (int, error) {
	n, present, err := parseOptionalInt(preferUnderscore(get, dotted, underscore), dotted)
	if err != nil {
		return 0, err
	}
	if !present || n <= 0 {
		return def, nil
	}
	return n, nil
}

func parseCooldown(get func(string) string) (int, error) {
	const def = 30
	n, present, err := parseOptionalInt(preferUnderscore(get, "generation.cooldown_seconds", "generation_cooldown_seconds"), "generation.cooldown_seconds")
	if err != nil {
		return 0, err
	}
	if !present {
		n = def
	}
	if n < 0 {
		return 0, fmt.Errorf("metaldogs plugin: generation.cooldown_seconds must be >= 0")
	}
	maxCooldown := int(domain.MaxGenerationCooldown / time.Second)
	if n > maxCooldown {
		return 0, fmt.Errorf("metaldogs plugin: generation.cooldown_seconds must be <= %d", maxCooldown)
	}
	return n, nil
}

func parseOptionalInt(raw, name string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, fmt.Errorf("metaldogs plugin: plugins.metaldogs.%s %q is not an int", name, raw)
	}
	return n, true, nil
}
