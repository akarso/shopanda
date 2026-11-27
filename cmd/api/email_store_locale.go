package main

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/translation"
)

// defaultStoreIDAndLocale returns store id and primary-language locale from one
// FindDefault call so claim / recovery / notification wiring cannot diverge.
func defaultStoreIDAndLocale(stores store.StoreRepository) func(context.Context) (storeID, locale string, err error) {
	return func(ctx context.Context) (string, string, error) {
		if stores == nil {
			return "", "", nil
		}
		st, err := stores.FindDefault(ctx)
		if err != nil {
			return "", "", err
		}
		if st == nil {
			return "", "", nil
		}
		return st.ID, translation.PrimaryLanguage(st.Language), nil
	}
}
