package metaldogs

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestValidateDesignLine(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	approved := domain.Design{Status: domain.DesignApproved, ExpiresAt: now.Add(time.Hour)}

	cases := []struct {
		name    string
		id      string
		present bool
		design  domain.Design
		found   bool
		code    string
		message string
	}{
		{name: "missing", present: false, code: ports.DesignRequiredCode, message: "design required"},
		{name: "blank", id: "  ", present: true, code: ports.DesignRequiredCode, message: "design required"},
		{name: "approved", id: "design-a", present: true, design: approved, found: true},
		{name: "ordered", id: "design-a", present: true, design: domain.Design{Status: domain.DesignOrdered, ExpiresAt: now.Add(time.Hour)}, found: true},
		{name: "draft", id: "design-a", present: true, design: domain.Design{Status: domain.DesignDraft, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignNotApprovedCode, message: "design is not approved"},
		{name: "generating", id: "design-a", present: true, design: domain.Design{Status: domain.DesignGenerating, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignNotApprovedCode, message: "design is not approved"},
		{name: "generation failed", id: "design-a", present: true, design: domain.Design{Status: domain.DesignGenerationFailed, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignNotApprovedCode, message: "design is not approved"},
		{name: "preview ready", id: "design-a", present: true, design: domain.Design{Status: domain.DesignPreviewReady, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignNotApprovedCode, message: "design is not approved"},
		{name: "expired status", id: "design-a", present: true, design: domain.Design{Status: domain.DesignExpired, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignExpiredCode, message: "design has expired"},
		{name: "expired clock", id: "design-a", present: true, design: domain.Design{Status: domain.DesignApproved, ExpiresAt: now.Add(-time.Minute)}, found: true, code: ports.DesignExpiredCode, message: "design has expired"},
		{name: "unknown", id: "design-a", present: true, found: false, code: ports.DesignNotFoundCode, message: "design not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue, blocked := ValidateDesignLine(DesignLineInput{
				Now: now, DesignID: tc.id, Present: tc.present, Design: tc.design, Found: tc.found,
			})
			if tc.code == "" {
				if blocked || issue != (DesignLineIssue{}) {
					t.Fatalf("issue=%+v blocked=%v", issue, blocked)
				}
				return
			}
			if !blocked || issue.Code != tc.code || issue.Message != tc.message {
				t.Fatalf("issue=%+v blocked=%v", issue, blocked)
			}
		})
	}
}
