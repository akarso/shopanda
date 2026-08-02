package http

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/search"
)

var storefrontSearchSuggestTemplate = template.Must(template.New("storefront-search-suggest").Parse(`
{{ if .Suggestions }}
<ul class="search-suggest-list">
    {{ range .Suggestions }}
    <li role="option"><a href="{{ .URL }}">{{ .Text }}</a></li>
    {{ end }}
</ul>
{{ else if .HasQuery }}
<p class="search-suggest-empty muted-note" role="status">No matching products.</p>
{{ end }}`))

type storefrontSearchSuggestData struct {
	HasQuery    bool
	Suggestions []storefrontSearchSuggestionItem
}

type storefrontSearchSuggestionItem struct {
	Text string
	URL  string
	Type string
}

// SearchSuggestFragment handles GET /fragments/search-suggest for storefront autocomplete.
func (h *StorefrontHandler) SearchSuggestFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if h.search == nil {
			return
		}
		prefix := strings.TrimSpace(r.URL.Query().Get("q"))
		data := storefrontSearchSuggestData{HasQuery: prefix != ""}
		if prefix != "" {
			suggestions, err := h.search.Suggest(r.Context(), prefix, search.DefaultSuggestLimit)
			if err != nil {
				h.log.Warn("storefront.search_suggest.failed", map[string]interface{}{
					"path":  r.URL.Path,
					"error": err.Error(),
				})
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			data.Suggestions = make([]storefrontSearchSuggestionItem, 0, len(suggestions))
			for _, item := range suggestions {
				data.Suggestions = append(data.Suggestions, storefrontSearchSuggestionItem{
					Text: item.Text,
					URL:  item.URL,
					Type: item.Type,
				})
			}
		}

		var buf bytes.Buffer
		if err := storefrontSearchSuggestTemplate.Execute(&buf, data); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(buf.Bytes())
	}
}
