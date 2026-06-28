package storecredit

import "errors"

// ErrInsufficientBalance is returned when a debit would exceed the account balance.
var ErrInsufficientBalance = errors.New("store credit: insufficient balance")
