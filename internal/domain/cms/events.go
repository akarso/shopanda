package cms

// Event names for the CMS domain.
const (
	EventPageCreated = "cms.page.created"
	EventPageUpdated = "cms.page.updated"
)

// PageCreatedData is the payload for cms.page.created.
type PageCreatedData struct {
	PageID string `json:"page_id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
}

// PageUpdatedData is the payload for cms.page.updated.
type PageUpdatedData struct {
	PageID string `json:"page_id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
}
