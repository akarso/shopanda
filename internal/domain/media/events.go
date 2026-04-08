package media

// Event names for media domain.
const (
	EventAssetUploaded = "asset.uploaded"
	EventAssetDeleted  = "asset.deleted"
)

// AssetEventData carries asset information in events.
type AssetEventData struct {
	AssetID  string
	Path     string
	Filename string
	MimeType string
}
