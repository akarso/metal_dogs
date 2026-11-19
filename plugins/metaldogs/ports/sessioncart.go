package ports

// DesignIDFieldCode is the cart-line extension written only by the add-to-cart route.
const DesignIDFieldCode = "metaldogs.design_id"

// Cart-validation issue codes. Level stays empty so a reported issue blocks that line.
const (
	DesignRequiredCode    = "metaldogs.design_required"
	DesignNotApprovedCode = "metaldogs.design_not_approved"
	DesignExpiredCode     = "metaldogs.design_expired"
	DesignNotFoundCode    = "metaldogs.design_not_found"
	// ProductUnavailableCode blocks a line whose parent is outside the catalog read scope.
	ProductUnavailableCode = "metaldogs.product_unavailable"
	// DesignMismatchCode is reserved for a template-to-product mapping (X2). It is not emitted yet.
	DesignMismatchCode = "metaldogs.design_mismatch"
)
