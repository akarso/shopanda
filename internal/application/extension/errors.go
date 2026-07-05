package extension

import (
	"errors"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// MapValueError converts domain extension value errors for HTTP mapping.
func MapValueError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domainext.ErrUnknownFieldCode) {
		return apperror.UnknownFieldCode(err.Error())
	}
	if errors.Is(err, domainext.ErrForbiddenPrivateField) {
		return apperror.ForbiddenPrivateField(err.Error())
	}
	if domainext.IsValidationError(err) {
		return apperror.FieldValidationFailed(err.Error())
	}
	return err
}
