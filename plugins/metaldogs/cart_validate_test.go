package metaldogs

import (
	"context"
	"testing"
	"time"

	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestOnCartValidate_EmptyCartWithoutSessionCart(t *testing.T) {
	current, err := domaincart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &current)
	if err := New().onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_MissingSessionCart(t *testing.T) {
	current, err := domaincart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	current.Items = []domaincart.Item{{VariantID: "variant-m", Quantity: 1}}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &current)
	if err := New().onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_OtherProductWithoutDesigns(t *testing.T) {
	current, err := domaincart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	current.Items = []domaincart.Item{{VariantID: "shirt", Quantity: 1}}
	cart := &scriptSessionCart{}
	p := New()
	p.sessionCart = cart
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &current)
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := cart.ctx.Deadline(); !ok {
		t.Fatal("validation I/O has no deadline")
	}
	if cart.lineCalls != 0 || cart.qtyCalls != 0 {
		t.Fatalf("line=%d qty=%d", cart.lineCalls, cart.qtyCalls)
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_TeeWithoutDesignRepository(t *testing.T) {
	current, err := domaincart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	current.Items = []domaincart.Item{{VariantID: "tee", Quantity: 1}}
	p := New()
	cart := &scriptSessionCart{tees: map[string]struct{}{"tee": {}}}
	p.sessionCart = cart
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &current)
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := cart.ctx.Deadline(); !ok {
		t.Fatal("validation I/O has no deadline")
	}
	issues := hookIssues(hctx)
	if len(issues) != 1 || issues[0].Code != ports.DesignRequiredCode || issues[0].Message != "design required" || issues[0].VariantID != "tee" || issues[0].Level != "" {
		t.Fatalf("issues=%+v", issues)
	}

	p.sessionCart = &scriptSessionCart{
		tees:  map[string]struct{}{"tee": {}},
		lines: map[string]string{"tee": "design-a"},
	}
	hctx = &extapi.HookContext{}
	hctx.Set("cart", &current)
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	issues = hookIssues(hctx)
	if len(issues) != 1 || issues[0].Code != ports.DesignNotFoundCode || issues[0].Message != "design not found" || issues[0].VariantID != "tee" || issues[0].Level != "" {
		t.Fatalf("issues=%+v", issues)
	}
}

type scriptSessionCart struct {
	tees      map[string]struct{}
	lines     map[string]string
	lineCalls int
	qtyCalls  int
	ctx       context.Context
}

func (s *scriptSessionCart) BelongsToCustomTee(ctx context.Context, variantID string) (bool, error) {
	lines, err := s.CustomTeeLines(ctx, []string{variantID})
	if err != nil {
		return false, err
	}
	_, ok := lines[variantID]
	return ok, nil
}

func (s *scriptSessionCart) CustomTeeLines(ctx context.Context, _ []string) (map[string]struct{}, error) {
	s.ctx = ctx
	if s.tees == nil {
		return map[string]struct{}{}, nil
	}
	return s.tees, nil
}

func (s *scriptSessionCart) PersistedLineQuantities(_ context.Context, _ string) (map[string]int, bool, error) {
	s.qtyCalls++
	return nil, false, nil
}

func (s *scriptSessionCart) LineDesign(_ context.Context, _, variantID string) (string, bool, error) {
	s.lineCalls++
	id, ok := s.lines[variantID]
	return id, ok, nil
}

func (s *scriptSessionCart) AddLine(_ context.Context, session ports.CartSession, _ ports.DesignLine) (ports.CartSession, ports.CartTarget, error) {
	return session, ports.CartTarget{}, nil
}

func hookIssues(hctx *extapi.HookContext) []extapi.CartValidationIssue {
	raw, _ := hctx.Get("validation_errors")
	issues, _ := raw.(*[]extapi.CartValidationIssue)
	if issues == nil {
		return nil
	}
	return *issues
}

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
