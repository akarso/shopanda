package media

// Event names for media domain.
const (
	EventAssetUploaded = "asset.uploaded"
	EventAssetDeleted  = "asset.deleted"
)

// AssetEventData carries asset information in events.
type AssetEventData struct {
	AssetID  string `json:"asset_id"`
	Path     string `json:"path"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
}
