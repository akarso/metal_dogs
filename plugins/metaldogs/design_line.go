package metaldogs

import (
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type designLineInput struct {
	Now      time.Time
	DesignID string
	Present  bool
	Design   domain.Design
	Found    bool
}

type designLineIssue struct {
	Code    string
	Message string
}

// The design store has no soft-delete, so a missing row is not found.
func validateDesignLine(in designLineInput) (designLineIssue, bool) {
	if !in.Present || strings.TrimSpace(in.DesignID) == "" {
		return designLineIssue{Code: ports.DesignRequiredCode, Message: "design required"}, true
	}
	if !in.Found {
		return designLineIssue{Code: ports.DesignNotFoundCode, Message: "design not found"}, true
	}
	if in.Design.Status == domain.DesignExpired || !in.Design.ExpiresAt.After(in.Now) {
		return designLineIssue{Code: ports.DesignExpiredCode, Message: "design has expired"}, true
	}
	if in.Design.Status != domain.DesignApproved && in.Design.Status != domain.DesignOrdered {
		return designLineIssue{Code: ports.DesignNotApprovedCode, Message: "design is not approved"}, true
	}
	return designLineIssue{}, false
}
