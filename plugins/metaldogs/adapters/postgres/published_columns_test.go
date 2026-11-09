package postgres

import (
	"strings"
	"testing"
)

func TestPublicTemplateColumns_OmitSecrets(t *testing.T) {
	t.Parallel()
	for _, leak := range []string{
		"prompt", "model_parameters", "content_hash",
		"reference_asset", "reference_pattern", "thumbnail_asset", "thumbnail_path",
		"status", "created_at",
	} {
		if strings.Contains(publicTemplateColumns, leak) {
			t.Fatalf("publicTemplateColumns must not include %q:\n%s", leak, publicTemplateColumns)
		}
	}
	for _, need := range []string{
		"id", "slug", "genre", "style_category", "version",
		"photo_requirements", "text_fields", "output_config", "published_at",
	} {
		if !strings.Contains(publicTemplateColumns, need) {
			t.Fatalf("publicTemplateColumns missing %q:\n%s", need, publicTemplateColumns)
		}
	}
}
