package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Design status values (ADR-0006).
const (
	DesignDraft            = "draft"
	DesignGenerating       = "generating"
	DesignGenerationFailed = "generation_failed"
	DesignPreviewReady     = "preview_ready"
	DesignApproved         = "approved"
	DesignOrdered          = "ordered"
	DesignExpired          = "expired"
)

// DesignStatuses is the golden list for docs/tests (ADR-0006).
var DesignStatuses = []string{
	DesignDraft,
	DesignGenerating,
	DesignGenerationFailed,
	DesignPreviewReady,
	DesignApproved,
	DesignOrdered,
	DesignExpired,
}

// DesignRetentionUnordered is how long never-ordered designs remain (domain-model).
const DesignRetentionUnordered = 30 * 24 * time.Hour

// SourceAttachedRetention is how long a design-attached source photo is kept from last activity.
const SourceAttachedRetention = 7 * 24 * time.Hour

// Design is the customer customizer aggregate.
type Design struct {
	ID                 string
	TemplateID         string
	TemplateVersion    int
	Status             string
	Texts              map[string]string
	SourceAssetID      string
	OwnerTokenHash     []byte // SHA-256 of owner_token; never expose
	CustomerID         string
	GenerationAttempts int
	LatestJobID        string
	ApprovedVersionID  string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ExpiresAt          time.Time
}

// ErrInvalidTransition is returned for illegal design status commands.
type ErrInvalidTransition struct {
	From, Command string
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid_transition: cannot %s from %s", e.Command, e.From)
}

// ErrInvalidTexts classifies customer text validation failures.
type ErrInvalidTexts struct {
	Message string
}

func (e *ErrInvalidTexts) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "invalid_texts"
}

// EditableForPatch reports whether UpdateTexts / ReplacePhoto are allowed.
func (d Design) EditableForPatch() bool {
	switch d.Status {
	case DesignDraft, DesignGenerationFailed, DesignPreviewReady:
		return true
	default:
		return false
	}
}

// AssertEditable returns ErrInvalidTransition when PATCH is not allowed.
func (d Design) AssertEditable(command string) error {
	if d.EditableForPatch() {
		return nil
	}
	return &ErrInvalidTransition{From: d.Status, Command: command}
}

// ApplyTexts updates texts while staying in an editable status (preview/failed → draft).
func (d *Design) ApplyTexts(texts map[string]string, at time.Time) error {
	if err := d.AssertEditable("UpdateTexts"); err != nil {
		return err
	}
	d.Texts = texts
	if d.Status == DesignPreviewReady || d.Status == DesignGenerationFailed {
		d.Status = DesignDraft
	}
	d.UpdatedAt = at.UTC()
	return nil
}

// ApplyPhoto replaces the source asset while staying editable (preview/failed → draft).
func (d *Design) ApplyPhoto(assetID string, at time.Time) error {
	if err := d.AssertEditable("ReplacePhoto"); err != nil {
		return err
	}
	if strings.TrimSpace(assetID) == "" {
		return fmt.Errorf("source asset id required")
	}
	d.SourceAssetID = assetID
	if d.Status == DesignPreviewReady || d.Status == DesignGenerationFailed {
		d.Status = DesignDraft
	}
	d.UpdatedAt = at.UTC()
	return nil
}

// TextFieldSpec describes one editable template field for runtime validation.
type TextFieldSpec struct {
	Key            string
	MaxLength      int
	AllowedCharset string
}

// ValidateDesignTexts checks customer input against editable specs and merges fixed copy.
// Unknown keys rejected; all editable keys required; fixed keys filled from fixed map.
func ValidateDesignTexts(editable []TextFieldSpec, fixed map[string]string, input map[string]string) (map[string]string, error) {
	if input == nil {
		input = map[string]string{}
	}
	allowed := make(map[string]TextFieldSpec, len(editable))
	for _, e := range editable {
		key := strings.TrimSpace(e.Key)
		if key == "" {
			continue
		}
		allowed[key] = TextFieldSpec{Key: key, MaxLength: e.MaxLength, AllowedCharset: e.AllowedCharset}
	}
	for k := range input {
		key := strings.TrimSpace(k)
		if _, ok := allowed[key]; !ok {
			// fixed keys must not be client-supplied either
			if _, isFixed := fixed[key]; isFixed {
				return nil, &ErrInvalidTexts{Message: fmt.Sprintf("text key %q is fixed", key)}
			}
			return nil, &ErrInvalidTexts{Message: fmt.Sprintf("unknown text key %q", key)}
		}
	}
	for key := range allowed {
		if _, isFixed := fixed[key]; isFixed {
			return nil, &ErrInvalidTexts{Message: fmt.Sprintf("text key %q is both editable and fixed", key)}
		}
	}
	out := make(map[string]string, len(allowed)+len(fixed))
	for key, spec := range allowed {
		raw, ok := input[key]
		if !ok {
			// also accept trimmed lookup
			for ik, iv := range input {
				if strings.TrimSpace(ik) == key {
					raw, ok = iv, true
					break
				}
			}
		}
		if !ok {
			return nil, &ErrInvalidTexts{Message: fmt.Sprintf("missing text key %q", key)}
		}
		val := strings.TrimSpace(raw)
		if val == "" {
			return nil, &ErrInvalidTexts{Message: fmt.Sprintf("text key %q is empty", key)}
		}
		if spec.MaxLength > 0 && len([]rune(val)) > spec.MaxLength {
			return nil, &ErrInvalidTexts{Message: fmt.Sprintf("text key %q exceeds max length %d", key, spec.MaxLength)}
		}
		if err := validateCharset(val, spec.AllowedCharset); err != nil {
			return nil, &ErrInvalidTexts{Message: fmt.Sprintf("text key %q: %v", key, err)}
		}
		out[key] = val
	}
	for k, v := range fixed {
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

func validateCharset(s, charset string) error {
	charset = strings.TrimSpace(charset)
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("control characters not allowed")
		}
		switch charset {
		case "ascii":
			if !isASCIINameRune(r) {
				return fmt.Errorf("charset ascii rejected rune %q", r)
			}
		case "latin":
			if !isLatinNameRune(r) {
				return fmt.Errorf("charset latin rejected rune %q", r)
			}
		case "latin_polish", "":
			if !isLatinPolishNameRune(r) {
				return fmt.Errorf("charset latin_polish rejected rune %q", r)
			}
		default:
			return fmt.Errorf("unknown charset %q", charset)
		}
	}
	return nil
}

func isASCIINameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == ' ' || r == '-'
}

func isLatinNameRune(r rune) bool {
	if isASCIINameRune(r) {
		return true
	}
	// Exclude Polish-only letters so charset "latin" is stricter than "latin_polish".
	// ó stays in latin (shared with other Western European names).
	if _, polishOnly := polishOnlyRunes[r]; polishOnly {
		return false
	}
	return unicode.Is(unicode.Latin, r) && unicode.IsLetter(r)
}

// polishOnlyRunes are Polish diacritics beyond generic Western European Latin (ó).
var polishOnlyRunes = map[rune]struct{}{
	'ą': {}, 'ć': {}, 'ę': {}, 'ł': {}, 'ń': {}, 'ś': {}, 'ź': {}, 'ż': {},
	'Ą': {}, 'Ć': {}, 'Ę': {}, 'Ł': {}, 'Ń': {}, 'Ś': {}, 'Ź': {}, 'Ż': {},
}

func isLatinPolishNameRune(r rune) bool {
	if isLatinNameRune(r) {
		return true
	}
	_, ok := polishOnlyRunes[r]
	return ok
}
