package catalog

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const downloadableFileMaxPosition = 9999
const downloadableFileMaxLabelLen = 200

// DownloadableRequiresFileMessage is the admin validation text when a
// downloadable product would become or remain StatusActive with zero
// attached media assets (PR-1074). Soft GUI warnings are PR-1078.
const DownloadableRequiresFileMessage = "downloadable products require at least one attached file before they can be active"

// DownloadableMissingFileNotPurchasableMessage is the cart/checkout text when
// a downloadable product has no attached file. Purchasable is also forced
// false via VisibilityInputs.MissingDownloadableFile.
const DownloadableMissingFileNotPurchasableMessage = "downloadable product has no attached file and is not purchasable"

// MissingDownloadableFileCode is the admin ExplainAxes code on the purchasable
// axis when a downloadable product has zero attachments. It is not an
// AutoBasis code and is not part of the shared PLP failure list.
const MissingDownloadableFileCode = "no_downloadable_file"

// DownloadableFile links a downloadable product to one media.Asset.
// Storage stays in the Media domain; this row is the product-to-asset
// attachment only (PR-1074). Grants and delivery are PR-1075 / PR-1077.
type DownloadableFile struct {
	ProductID string
	AssetID   string
	Position  int
	Label     string
	CreatedAt time.Time
}

// NewDownloadableFile builds an attachment with Position defaulting to 0 and
// an empty Label. IDs are trimmed; field validation runs before return.
func NewDownloadableFile(productID, assetID string) (*DownloadableFile, error) {
	f := &DownloadableFile{
		ProductID: strings.TrimSpace(productID),
		AssetID:   strings.TrimSpace(assetID),
		Position:  0,
		Label:     "",
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return f, nil
}

// Validate checks field-level constraints before persist.
// It trims ProductID and AssetID in place so Upsert callers that skip
// NewDownloadableFile / Attach still persist clean IDs.
func (f *DownloadableFile) Validate() error {
	if f == nil {
		return errors.New("downloadable file must not be nil")
	}
	f.ProductID = strings.TrimSpace(f.ProductID)
	f.AssetID = strings.TrimSpace(f.AssetID)
	f.Label = strings.TrimSpace(f.Label)
	if f.ProductID == "" {
		return errors.New("product id must not be empty")
	}
	if f.AssetID == "" {
		return errors.New("asset id must not be empty")
	}
	if f.Position < 0 {
		return errors.New("position must not be negative")
	}
	if f.Position > downloadableFileMaxPosition {
		return errors.New("position must not exceed 9999")
	}
	if len([]rune(f.Label)) > downloadableFileMaxLabelLen {
		return fmt.Errorf("label must not exceed %d characters", downloadableFileMaxLabelLen)
	}
	return nil
}

// ValidateAttachment checks that the product type may own downloadable files.
// Call after loading the product from persistence.
func (f *DownloadableFile) ValidateAttachment(productType Type) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if productType != TypeDownloadable {
		return fmt.Errorf("only products with type %q can have downloadable files", TypeDownloadable)
	}
	return nil
}

// DownloadableMissingFileBlocksPurchase reports the purchasable-axis override
// for a downloadable product with zero attached files. A nil product or a
// non-downloadable type never blocks. fileCount < 1 blocks.
func DownloadableMissingFileBlocksPurchase(p *Product, fileCount int) bool {
	if p == nil || p.Type != TypeDownloadable {
		return false
	}
	return fileCount < 1
}
