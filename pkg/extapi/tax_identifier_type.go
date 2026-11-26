package extapi

// TaxIdentifierType is a plugin-facing tax/company identifier catalog entry
// (PR-1201). Code must be a snake token (e.g. pl_nip).
type TaxIdentifierType struct {
	Code       string
	Label      string
	Countries  []string // ISO-2; empty = "Other"
	Region     string   // "EU" | "US" | ""
	FormatHint string
}
