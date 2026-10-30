package download

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// GrantSettings is an optional per-downloadable-product override of the
// global catalog.downloadable defaults (PR-1075). A missing row, or a NULL
// column, means "use global." Admin write HTTP is PR-1078.
type GrantSettings struct {
	ProductID     string
	ExpiryDays    *int // nil → global default expiry
	DownloadLimit *int // nil → global default limit
}

// Defaults are the global fallbacks from catalog.downloadable config.
type Defaults struct {
	Expiry        time.Duration
	DownloadLimit int
}

// ValidateDefaults checks global config values before wiring.
func ValidateDefaults(d Defaults) error {
	if d.Expiry <= 0 {
		return errors.New("download grant defaults: expiry must be positive")
	}
	if d.DownloadLimit < 1 {
		return errors.New("download grant defaults: download_limit must be at least 1")
	}
	if d.DownloadLimit > MaxDownloadLimit {
		return fmt.Errorf("download grant defaults: download_limit must not exceed %d", MaxDownloadLimit)
	}
	return nil
}

// Validate checks optional product overrides. Both override pointers may be nil.
func (s *GrantSettings) Validate() error {
	if s == nil {
		return errors.New("download grant settings must not be nil")
	}
	s.ProductID = strings.TrimSpace(s.ProductID)
	if s.ProductID == "" {
		return errors.New("download grant settings: product id must not be empty")
	}
	if s.ExpiryDays != nil {
		if *s.ExpiryDays < 1 {
			return errors.New("download grant settings: expiry_days must be at least 1")
		}
		if *s.ExpiryDays > MaxExpiryDays {
			return fmt.Errorf("download grant settings: expiry_days must not exceed %d", MaxExpiryDays)
		}
	}
	if s.DownloadLimit != nil {
		if *s.DownloadLimit < 1 {
			return errors.New("download grant settings: download_limit must be at least 1")
		}
		if *s.DownloadLimit > MaxDownloadLimit {
			return fmt.Errorf("download grant settings: download_limit must not exceed %d", MaxDownloadLimit)
		}
	}
	return nil
}

// Resolve merges product overrides onto global defaults. settings may be nil
// (no row). Out-of-range overrides are rejected; they are not silently clamped.
func Resolve(settings *GrantSettings, global Defaults) (expiresIn time.Duration, limit int, err error) {
	if err := ValidateDefaults(global); err != nil {
		return 0, 0, err
	}
	expiresIn = global.Expiry
	limit = global.DownloadLimit
	if settings == nil {
		return expiresIn, limit, nil
	}
	if err := settings.Validate(); err != nil {
		return 0, 0, err
	}
	if settings.ExpiryDays != nil {
		expiresIn = time.Duration(*settings.ExpiryDays) * 24 * time.Hour
	}
	if settings.DownloadLimit != nil {
		limit = *settings.DownloadLimit
	}
	return expiresIn, limit, nil
}
