package metaldogs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

const metalDogsSlug = "metaldogs"

// Settings is the effective plugins.metaldogs configuration.
type Settings struct {
	Enabled              bool
	Provider             string
	MaxRetries           int
	MaxAttemptsPerDesign int
	CooldownSeconds      int
}

// Enabled reports whether plugins.metaldogs.enabled is on.
// A missing value is off. Values other than 1/true/yes and 0/false/no are errors.
func Enabled(cfg *config.Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	return ParseEnabled(cfg.Plugins.Extra[metalDogsSlug]["enabled"])
}

// ParseEnabled interprets the Extra string for plugins.metaldogs.enabled.
func ParseEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "no":
		return false, nil
	case "1", "true", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("metaldogs plugin: plugins.metaldogs.enabled %q is not a bool", raw)
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
	return Settings{
		Enabled:              enabled,
		Provider:             provider,
		MaxRetries:           maxRetries,
		MaxAttemptsPerDesign: maxAttempts,
		CooldownSeconds:      cooldown,
	}, nil
}

func metalDogsKey(key string) string {
	return "plugins." + metalDogsSlug + "." + key
}

// Env writes the all-underscore key. Nested YAML also leaves a mixed key
// (generation.max_retries) that setPluginExtraKey does not overwrite.
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
