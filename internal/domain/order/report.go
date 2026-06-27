package order

import "time"

// TaxSnapshotRow is a lightweight paid-order tax row for OSS/IOSS CSV exports.
type TaxSnapshotRow struct {
	OrderID            string
	CreatedAt          time.Time
	DestinationCountry string
	Currency           string
	SubtotalAmount     int64
	TaxAmount          int64
}
