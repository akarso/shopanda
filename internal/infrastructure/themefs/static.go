package themefs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/theme"
)

// OverlayStaticRoot prepends themeDir/static when that directory exists.
func OverlayStaticRoot(merged *theme.ResolvedTemplates, themeDir string) {
	if merged == nil {
		return
	}
	staticDir := filepath.Join(themeDir, "static")
	info, err := os.Stat(staticDir)
	if err != nil || !info.IsDir() {
		return
	}
	merged.StaticRoots = append([]string{staticDir}, merged.StaticRoots...)
}

// ResolveStaticFile returns the first existing non-directory file for rel
// along roots (child first). rel is slash-separated and must stay inside root.
func ResolveStaticFile(roots []string, rel string) (string, error) {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return "", os.ErrNotExist
	}
	for _, root := range roots {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if !theme.IsPathWithinBoundary(full, root) {
			continue
		}
		info, err := os.Lstat(full)
		if err != nil || info.IsDir() {
			continue
		}
		real, err := filepath.EvalSymlinks(full)
		if err != nil {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			realRoot = root
		}
		if !theme.IsPathWithinBoundary(real, realRoot) {
			continue
		}
		realInfo, err := os.Stat(real)
		if err != nil || realInfo.IsDir() {
			continue
		}
		return real, nil
	}
	return "", os.ErrNotExist
}

// ContentVersion is a short sha256 of the file contents.
func ContentVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return ""
	}
	return hex.EncodeToString(sum.Sum(nil))[:12]
}

// StaticURL is a /static/ path with a content-hash cache buster resolved now.
// A CSS/JS edit changes the next HTML render without a process restart.
func StaticURL(roots []string, rel string) string {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	path := "/static/" + rel
	if rel == "" {
		return path
	}
	full, err := ResolveStaticFile(roots, rel)
	if err != nil {
		return path
	}
	if v := ContentVersion(full); v != "" {
		return path + "?v=" + v
	}
	return path
}

// StaticHandler serves /static/{path...} from inherited theme static roots.
// roots is called per request so a watched engine swap is visible.
func StaticHandler(roots func() []string, noStore bool) http.Handler {
	if roots == nil {
		roots = func() []string { return nil }
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean("/"+r.PathValue("path")), "/")
		full, err := ResolveStaticFile(roots(), rel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if noStore {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		}
		http.ServeFile(w, r, full)
	})
}
