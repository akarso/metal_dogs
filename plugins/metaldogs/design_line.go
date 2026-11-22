package metaldogs

import (
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// DesignLineInput is the sale check shared by cart.validate and checkout.
// Present is whether the line carried a design id; Found is whether the store returned that design.
type DesignLineInput struct {
	Now      time.Time
	DesignID string
	Present  bool
	Design   domain.Design
	Found    bool
}

// DesignLineIssue is a blocking sale failure. Code and Message match the cart hook.
type DesignLineIssue struct {
	Code    string
	Message string
}

// ValidateDesignLine decides whether a custom-tee line may be sold.
// A design the store does not return (missing row; this store has no soft-delete) is not found.
func ValidateDesignLine(in DesignLineInput) (DesignLineIssue, bool) {
	if !in.Present || strings.TrimSpace(in.DesignID) == "" {
		return DesignLineIssue{Code: ports.DesignRequiredCode, Message: "design required"}, true
	}
	if !in.Found {
		return DesignLineIssue{Code: ports.DesignNotFoundCode, Message: "design not found"}, true
	}
	if in.Design.Status == domain.DesignExpired || !in.Design.ExpiresAt.After(in.Now) {
		return DesignLineIssue{Code: ports.DesignExpiredCode, Message: "design has expired"}, true
	}
	if in.Design.Status != domain.DesignApproved && in.Design.Status != domain.DesignOrdered {
		return DesignLineIssue{Code: ports.DesignNotApprovedCode, Message: "design is not approved"}, true
	}
	return DesignLineIssue{}, false
}
