package browser

import (
	"embed"
	"os"
	"strings"
)

//go:embed sitepatterns/*.md
var sitePatternsFS embed.FS

// SyncBuiltinSitePatterns copies built-in site patterns to the user's
// site-patterns directory. Existing files are never overwritten so that
// user-customised experience files are preserved.
func SyncBuiltinSitePatterns() int {
	dir := SiteExperienceDir()
	os.MkdirAll(dir, 0755)

	entries, err := sitePatternsFS.ReadDir("sitepatterns")
	if err != nil {
		return 0
	}

	synced := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "_") {
			continue // skip template files
		}
		domain := strings.TrimSuffix(entry.Name(), ".md")
		targetPath := SiteExperiencePath(domain)

		// Never overwrite existing user patterns
		if _, err := os.Stat(targetPath); err == nil {
			continue
		}

		data, err := sitePatternsFS.ReadFile("sitepatterns/" + entry.Name())
		if err != nil {
			continue
		}
		if err := os.WriteFile(targetPath, data, 0644); err == nil {
			synced++
		}
	}
	return synced
}
