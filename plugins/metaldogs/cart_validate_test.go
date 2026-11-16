package metaldogs

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestDesignCartIssue(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	variant := "variant-m"
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
		{name: "not approved", id: "design-a", present: true, design: domain.Design{Status: domain.DesignOrdered, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignNotApprovedCode, message: "design is not approved"},
		{name: "expired status", id: "design-a", present: true, design: domain.Design{Status: domain.DesignExpired, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignExpiredCode, message: "design has expired"},
		{name: "expired clock", id: "design-a", present: true, design: domain.Design{Status: domain.DesignApproved, ExpiresAt: now.Add(-time.Minute)}, found: true, code: ports.DesignExpiredCode, message: "design has expired"},
		{name: "unknown", id: "design-a", present: true, found: false, code: ports.DesignMismatchCode, message: "design does not match the custom tee"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue, blocked := designCartIssue(now, variant, tc.id, tc.present, tc.design, tc.found)
			if tc.code == "" {
				if blocked {
					t.Fatalf("issue=%+v", issue)
				}
				return
			}
			if !blocked || issue.Code != tc.code || issue.Message != tc.message || issue.VariantID != variant || issue.Level != "" {
				t.Fatalf("issue=%+v blocked=%v", issue, blocked)
			}
		})
	}
}
