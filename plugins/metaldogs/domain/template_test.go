package domain_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

func validTemplate() domain.Template {
	return domain.Template{
		Slug:          "head-front-blackmetal",
		Genre:         "black_metal",
		StyleCategory: "head_portrait",
		PromptInstructions: domain.PromptInstructions{
			Text:          "prompt",
			PromptVersion: "g2-v1",
		},
		ModelParameters: domain.ModelParameters{
			Model: "gpt-image-2.5-sunburst", Width: 1024, Height: 1024, Seed: 1,
		},
		PhotoRequirements: domain.PhotoRequirements{
			Pose: "head_front",
		},
		TextFields: domain.TextFields{
			Editable: []domain.EditableText{{
				Key: "dog_name", MaxLength: 12, AllowedCharset: "latin_polish",
			}},
			Fixed: []domain.FixedText{},
		},
		OutputConfig: domain.OutputConfig{
			CanvasWidth: 1024, CanvasHeight: 1024, Monochrome: true,
			PrintArea: domain.PrintArea{X: 0, Y: 0, Width: 1024, Height: 1024},
		},
	}
}

func TestTemplate_Validate_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*domain.Template)
		wantErr string
	}{
		{name: "ok", mutate: func(*domain.Template) {}},
		{name: "bad slug", mutate: func(t *domain.Template) { t.Slug = "Bad Slug" }, wantErr: "invalid slug"},
		{name: "missing pose", mutate: func(t *domain.Template) { t.PhotoRequirements.Pose = "" }, wantErr: "unknown photo pose"},
		{name: "partial WxH", mutate: func(t *domain.Template) { t.ModelParameters.Height = 0 }, wantErr: "both be set"},
		{name: "missing model", mutate: func(t *domain.Template) { t.ModelParameters.Model = "" }, wantErr: "model required"},
		{name: "bad charset", mutate: func(tpl *domain.Template) {
			tpl.TextFields.Editable[0].AllowedCharset = "emoji"
		}, wantErr: "allowed_charset"},
		{name: "no dog_name", mutate: func(tpl *domain.Template) {
			tpl.TextFields.Editable[0].Key = "band_name"
		}, wantErr: "dog_name"},
		{name: "dup dog_name", mutate: func(tpl *domain.Template) {
			tpl.TextFields.Editable = append(tpl.TextFields.Editable, domain.EditableText{
				Key: "dog_name", MaxLength: 8, AllowedCharset: "latin_polish",
			})
		}, wantErr: "duplicate editable"},
		{name: "print OOB", mutate: func(tpl *domain.Template) {
			tpl.OutputConfig.PrintArea.Width = 2000
		}, wantErr: "print_area outside"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tpl := validTemplate()
			tc.mutate(&tpl)
			err := tpl.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestTemplate_ContentHash_StableAndSensitive(t *testing.T) {
	t.Parallel()
	a := validTemplate()
	b := validTemplate()
	ha, err := a.ContentHashDigest()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.ContentHashDigest()
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("identical defs differ: %s vs %s", ha, hb)
	}
	b.PromptInstructions.Text = "changed"
	hc, err := b.ContentHashDigest()
	if err != nil {
		t.Fatal(err)
	}
	if hc == ha {
		t.Fatal("prompt edit must change hash")
	}
}

func TestTemplate_ContentHash_NilFixedEqualsEmpty(t *testing.T) {
	t.Parallel()
	a := validTemplate()
	a.TextFields.Fixed = nil
	b := validTemplate()
	b.TextFields.Fixed = []domain.FixedText{}
	ha, err := a.ContentHashDigest()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.ContentHashDigest()
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("nil vs empty fixed must hash equal: %s vs %s", ha, hb)
	}
}
