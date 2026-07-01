package app

import (
	"net/http"
	"strings"
)

// robotsTxt serves a conservative policy: only the marketing pages are crawlable,
// secret and API routes are disallowed. robots.txt isn't our real indexing control
// (noindex headers are), but keeping crawlers off the API is still worthwhile.
func robotsTxt(baseURL string) http.HandlerFunc {
	body := strings.Join([]string{
		"User-agent: *",
		"Allow: /$",
		"Allow: /security",
		"Allow: /privacy",
		"Disallow: /p/",
		"Disallow: /s/",
		"Disallow: /live/",
		"Disallow: /manage/",
		"Disallow: /api/",
		"Disallow: /diff",
		"",
		"Sitemap: " + strings.TrimRight(baseURL, "/") + "/sitemap.xml",
		"",
	}, "\n")
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

// sitemapXML lists only the indexable public pages, never secret paste routes.
func sitemapXML(baseURL string) http.HandlerFunc {
	base := strings.TrimRight(baseURL, "/")
	pages := []string{"/", "/security", "/privacy"}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range pages {
		b.WriteString("  <url><loc>" + base + p + "</loc></url>\n")
	}
	b.WriteString(`</urlset>` + "\n")
	body := b.String()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}
