package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

func TestDesignStatuses_GoldenADR0006(t *testing.T) {
	t.Parallel()
	want := []string{
		"draft", "generating", "generation_failed", "preview_ready",
		"approved", "ordered", "expired",
	}
	if len(domain.DesignStatuses) != len(want) {
		t.Fatalf("len=%d want %d", len(domain.DesignStatuses), len(want))
	}
	for i := range want {
		if domain.DesignStatuses[i] != want[i] {
			t.Fatalf("[%d]=%q want %q", i, domain.DesignStatuses[i], want[i])
		}
	}
}

func TestDesign_AssertEditable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		ok     bool
	}{
		{domain.DesignDraft, true},
		{domain.DesignGenerationFailed, true},
		{domain.DesignPreviewReady, true},
		{domain.DesignGenerating, false},
		{domain.DesignApproved, false},
		{domain.DesignOrdered, false},
		{domain.DesignExpired, false},
	}
	for _, tc := range cases {
		d := domain.Design{Status: tc.status}
		err := d.AssertEditable("UpdateTexts")
		if tc.ok && err != nil {
			t.Fatalf("status %s: %v", tc.status, err)
		}
		if !tc.ok {
			var it *domain.ErrInvalidTransition
			if !errors.As(err, &it) {
				t.Fatalf("status %s: want invalid_transition, got %v", tc.status, err)
			}
		}
	}
}

func TestDesign_ApplyTexts_ResetsPreviewToDraft(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	d := domain.Design{Status: domain.DesignPreviewReady, Texts: map[string]string{"dog_name": "A"}}
	if err := d.ApplyTexts(map[string]string{"dog_name": "Burek"}, now); err != nil {
		t.Fatal(err)
	}
	if d.Status != domain.DesignDraft || d.Texts["dog_name"] != "Burek" || !d.UpdatedAt.Equal(now) {
		t.Fatalf("%+v", d)
	}
}

func TestValidateDesignTexts(t *testing.T) {
	t.Parallel()
	editable := []domain.TextFieldSpec{{
		Key: "dog_name", MaxLength: 12, AllowedCharset: "latin_polish",
	}}
	fixed := map[string]string{"tagline": "METAL"}
	out, err := domain.ValidateDesignTexts(editable, fixed, map[string]string{"dog_name": " Burek "})
	if err != nil || out["dog_name"] != "Burek" || out["tagline"] != "METAL" {
		t.Fatalf("out=%v err=%v", out, err)
	}

	_, err = domain.ValidateDesignTexts(editable, fixed, map[string]string{"dog_name": "Burek", "extra": "x"})
	if err == nil {
		t.Fatal("unknown key")
	}
	_, err = domain.ValidateDesignTexts(editable, fixed, map[string]string{})
	if err == nil {
		t.Fatal("missing key")
	}
	_, err = domain.ValidateDesignTexts(editable, fixed, map[string]string{"dog_name": "ąćęłńóśźżOK"})
	if err != nil {
		t.Fatalf("polish: %v", err)
	}
	_, err = domain.ValidateDesignTexts(
		[]domain.TextFieldSpec{{Key: "dog_name", MaxLength: 12, AllowedCharset: "latin"}},
		nil,
		map[string]string{"dog_name": "Burekł"},
	)
	if err == nil {
		t.Fatal("latin must reject Polish-only ł")
	}
	_, err = domain.ValidateDesignTexts(
		[]domain.TextFieldSpec{{Key: "dog_name", MaxLength: 12, AllowedCharset: "latin"}},
		nil,
		map[string]string{"dog_name": "León"},
	)
	if err != nil {
		t.Fatalf("latin should allow ó: %v", err)
	}
	_, err = domain.ValidateDesignTexts(
		[]domain.TextFieldSpec{{Key: "dog_name", MaxLength: 5, AllowedCharset: "ascii"}},
		nil,
		map[string]string{"dog_name": "Burek!"},
	)
	if err == nil {
		t.Fatal("ascii reject !")
	}
}
