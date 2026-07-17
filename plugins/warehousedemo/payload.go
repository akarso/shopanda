package warehousedemo

// StockResponse is the mock warehouse GET /stock JSON envelope.
type StockResponse struct {
	Stock []StockRow `json:"stock"`
}

// StockRow is one absolute stock level keyed by variant SKU.
type StockRow struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}
