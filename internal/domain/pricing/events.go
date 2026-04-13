package pricing

// Event names for the pricing domain.
const (
	EventPriceUpserted = "pricing.price.upserted"
)

// PriceUpsertedData is the payload for pricing.price.upserted.
type PriceUpsertedData struct {
	PriceID   string `json:"price_id"`
	VariantID string `json:"variant_id"`
	ProductID string `json:"product_id"`
	StoreID   string `json:"store_id"`
	Currency  string `json:"currency"`
	Amount    int64  `json:"amount"`
}
