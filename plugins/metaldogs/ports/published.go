package ports

import (
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// PublishedTemplate is the public-read projection of a published catalog row.
// It never carries prompts, model parameters, content hashes, or file paths.
type PublishedTemplate struct {
	ID                string
	Slug              string
	Genre             string
	StyleCategory     string
	Version           int
	PublishedAt       *time.Time
	PhotoRequirements domain.PhotoRequirements
	TextFields        domain.TextFields
	OutputConfig      domain.OutputConfig
}
