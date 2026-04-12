package cms

// Event names for the CMS domain.
const (
	EventPageCreated = "cms.page.created"
	EventPageUpdated = "cms.page.updated"
	EventPageDeleted = "cms.page.deleted"
)

// PageCreatedData is the payload for cms.page.created.
type PageCreatedData struct {
	PageID string `json:"page_id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// PageUpdatedData is the payload for cms.page.updated.
type PageUpdatedData struct {
	PageID string `json:"page_id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// PageDeletedData is the payload for cms.page.deleted.
type PageDeletedData struct {
	PageID string `json:"page_id"`
	Slug   string `json:"slug"`
}
