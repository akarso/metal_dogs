package ports

import (
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// Published* nested types are port-owned (no json tags). Domain JSON tags never
// sit on the public-read model; adapters map DB jsonb → these fields, httpapi
// maps to transport DTOs.

type PublishedPhotoRequirements struct {
	Pose           string
	BackgroundHint string
	LightingHint   string
}

type PublishedEditableText struct {
	Key            string
	MaxLength      int
	AllowedCharset string
}

type PublishedFixedText struct {
	Key   string
	Value string
}

type PublishedTextFields struct {
	Editable []PublishedEditableText
	Fixed    []PublishedFixedText
}

type PublishedPrintArea struct {
	X, Y, Width, Height int
}

type PublishedOutputConfig struct {
	CanvasWidth  int
	CanvasHeight int
	Monochrome   bool
	PrintArea    PublishedPrintArea
}

// PublishedTemplate is the public-read projection of a published catalog row.
// It never carries prompts, model parameters, content hashes, or file paths.
type PublishedTemplate struct {
	ID                string
	Slug              string
	Genre             string
	StyleCategory     string
	Version           int
	PublishedAt       *time.Time
	PhotoRequirements PublishedPhotoRequirements
	TextFields        PublishedTextFields
	OutputConfig      PublishedOutputConfig
}

// PublishedFromDomain copies only guest-safe fields from a full domain Template
// (for test fakes and adapters that already hold a domain row).
func PublishedFromDomain(t domain.Template) PublishedTemplate {
	editable := make([]PublishedEditableText, 0, len(t.TextFields.Editable))
	for _, e := range t.TextFields.Editable {
		editable = append(editable, PublishedEditableText{
			Key: e.Key, MaxLength: e.MaxLength, AllowedCharset: e.AllowedCharset,
		})
	}
	fixed := make([]PublishedFixedText, 0, len(t.TextFields.Fixed))
	for _, f := range t.TextFields.Fixed {
		fixed = append(fixed, PublishedFixedText{Key: f.Key, Value: f.Value})
	}
	return PublishedTemplate{
		ID: t.ID, Slug: t.Slug, Genre: t.Genre, StyleCategory: t.StyleCategory,
		Version: t.Version, PublishedAt: t.PublishedAt,
		PhotoRequirements: PublishedPhotoRequirements{
			Pose: t.PhotoRequirements.Pose, BackgroundHint: t.PhotoRequirements.BackgroundHint,
			LightingHint: t.PhotoRequirements.LightingHint,
		},
		TextFields: PublishedTextFields{Editable: editable, Fixed: fixed},
		OutputConfig: PublishedOutputConfig{
			CanvasWidth: t.OutputConfig.CanvasWidth, CanvasHeight: t.OutputConfig.CanvasHeight,
			Monochrome: t.OutputConfig.Monochrome,
			PrintArea: PublishedPrintArea{
				X: t.OutputConfig.PrintArea.X, Y: t.OutputConfig.PrintArea.Y,
				Width: t.OutputConfig.PrintArea.Width, Height: t.OutputConfig.PrintArea.Height,
			},
		},
	}
}
