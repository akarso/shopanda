package storeidentity

import (
	"context"
	"strings"

	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/store"
)

// EmailBrandSource fills mail.EmailData brand fields for async jobs
// (cart recovery) that are outside the notification service.
type EmailBrandSource struct {
	Identity *Service
	Stores   store.StoreRepository
	Config   legal.ConfigGetter // optional store.logo until PR-1222
}

// BrandEmailData returns StoreName/StoreAddress/LogoURL/StoreURL for templates.
// Failures are best-effort — StoreURL is always set from the argument.
func (b EmailBrandSource) BrandEmailData(ctx context.Context, storeURL string) mail.EmailData {
	ed := mail.EmailData{StoreURL: storeURL}
	if b.Stores == nil {
		return ed
	}
	st, err := b.Stores.FindDefault(ctx)
	if err != nil || st == nil {
		return ed
	}
	ed.StoreName = strings.TrimSpace(st.Name)
	if b.Identity != nil {
		addr, err := b.Identity.FormattedGeneralAddress(ctx, st.ID)
		if err == nil {
			ed.StoreAddress = addr
		}
	}
	ed.LogoURL = logoURL(ctx, b.Config, st.ID)
	return ed
}

func logoURL(ctx context.Context, cfg legal.ConfigGetter, storeID string) string {
	if cfg == nil {
		return ""
	}
	read := func(key string) string {
		v, err := cfg.Get(ctx, key)
		if err != nil || v == nil {
			return ""
		}
		t, ok := v.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(t)
	}
	if storeID != "" {
		if u := read(legal.ScopedConfigKey(storeID, "store.logo")); u != "" {
			return u
		}
	}
	return read("store.logo")
}
