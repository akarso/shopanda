package main

import (
	"context"
	"fmt"
)

// errBootInterrupted returns a non-nil error when a signal.NotifyContext was
// cancelled during boot. Call after Stop so the default SIGTERM terminate
// behaviour is restored for later phases; still refuse to continue if the
// signal arrived while NotifyContext had disabled that default.
func errBootInterrupted(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("boot interrupted: %w", err)
	}
	return nil
}
