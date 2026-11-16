package metaldogs

import (
	"testing"
	"time"

	domaincart "github.com/akarso/shopanda/internal/domain/cart"
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
		{name: "unknown", id: "design-a", present: true, found: false, code: ports.DesignNotFoundCode, message: "design not found"},
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

func TestTeeLinesToValidate(t *testing.T) {
	tees := map[string]struct{}{"m": {}, "l": {}}
	stored := map[string]int{"m": 1, "l": 1}
	proposed := []domaincart.Item{{VariantID: "m", Quantity: 1}, {VariantID: "l", Quantity: 1}}

	if got := teeLinesToValidate(proposed, stored, true, tees); len(got) != 2 {
		t.Fatalf("read path = %v", got)
	}
	removed := []domaincart.Item{{VariantID: "l", Quantity: 1}}
	if got := teeLinesToValidate(removed, stored, true, tees); len(got) != 0 {
		t.Fatalf("unchanged stale line = %v", got)
	}
	changed := []domaincart.Item{{VariantID: "m", Quantity: 2}, {VariantID: "l", Quantity: 1}}
	got := teeLinesToValidate(changed, stored, true, tees)
	if len(got) != 1 {
		t.Fatalf("changed line = %v", got)
	}
	if _, ok := got["m"]; !ok {
		t.Fatalf("changed line = %v", got)
	}
	added := []domaincart.Item{{VariantID: "m", Quantity: 1}, {VariantID: "l", Quantity: 1}, {VariantID: "x", Quantity: 1}}
	if got := teeLinesToValidate(added, stored, true, tees); len(got) != 0 {
		t.Fatalf("other product = %v", got)
	}
	bare := []domaincart.Item{{VariantID: "m", Quantity: 1}}
	got = teeLinesToValidate(bare, map[string]int{"shirt": 1}, true, map[string]struct{}{"m": {}})
	if len(got) != 1 {
		t.Fatalf("new tee = %v", got)
	}
	if _, ok := got["m"]; !ok {
		t.Fatalf("new tee = %v", got)
	}
	if got := teeLinesToValidate(proposed, nil, false, tees); len(got) != 2 {
		t.Fatalf("missing cart = %v", got)
	}
}
