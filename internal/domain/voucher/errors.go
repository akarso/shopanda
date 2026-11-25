package voucher

import "errors"

// Sentinel errors for redeem/unredeem gates. Application maps with errors.Is.
var (
	ErrVoid              = errors.New("voucher: void")
	ErrExhausted         = errors.New("voucher: fully redeemed")
	ErrNothingToUnredeem = errors.New("voucher: nothing to unredeem")
	ErrNotEnabled        = errors.New("voucher: product is not an enabled voucher")
	ErrNotVirtual        = errors.New("voucher: product must be virtual")
	// ErrCodeTaken is a unique violation on vouchers.code — retry with a new code
	// inside a savepoint so the surrounding order TX is not aborted.
	ErrCodeTaken = errors.New("voucher: code already taken")
	// ErrAlreadyIssued is a unique violation on (order_id, variant_id, seq).
	ErrAlreadyIssued = errors.New("voucher: already issued for order line sequence")
	// ErrOnBlockingReturn is returned when redeem is attempted while the
	// order line sits on an open or refunded return.
	ErrOnBlockingReturn = errors.New("voucher: order line is on a blocking return")
	// ErrRedeemedOnReturn is returned when approve/refund would void a
	// code that already has redemptions.
	ErrRedeemedOnReturn = errors.New("voucher: cannot void a redeemed voucher on return")
)
