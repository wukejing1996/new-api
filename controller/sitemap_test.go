package controller

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSitemapCanonicalArticlesAndAccurateDates(t *testing.T) {
	updatedAt := time.Date(2026, 10, 2, 18, 30, 0, 0, time.UTC).Unix()
	posts := []model.BlogPostListItem{
		{Slug: "first-article", UpdatedAt: updatedAt},
		{Slug: "new-article"},
		{Slug: "", UpdatedAt: updatedAt},
	}
	urls := buildSitemapURLs("https://costrouter.ai", posts)
	require.Len(t, urls, 50)
	seen := make(map[string]bool, len(urls))
	for _, item := range urls {
		assert.False(t, seen[item.Loc], "duplicate sitemap URL: %s", item.Loc)
		seen[item.Loc] = true
		assert.NotContains(t, item.Loc, "/dashboard")
		assert.NotContains(t, item.Loc, "/zh")
		if !strings.Contains(item.Loc, "/blog/") {
			assert.Empty(t, item.LastMod, "static pages must not claim a fresh modification on each request")
		}
	}
	for _, locale := range []string{"en", "fr", "ja", "ko", "ru", "vi"} {
		assert.True(t, seen["https://costrouter.ai/"+locale+"/enterprise"])
	}
	assert.Equal(t, sitemapURL{Loc: "https://costrouter.ai/en/blog/first-article", LastMod: "2026-10-02"}, urls[48])
	assert.Equal(t, sitemapURL{Loc: "https://costrouter.ai/en/blog/new-article"}, urls[49])

	payload, err := xml.Marshal(sitemapURLSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: urls})
	require.NoError(t, err)
	var decoded sitemapURLSet
	require.NoError(t, xml.Unmarshal(payload, &decoded))
	assert.Equal(t, urls, decoded.URLs)
	assert.NotContains(t, string(payload), "<lastmod></lastmod>")

	// A newly published post joins the generated sitemap without a static file update.
	posts = append(posts, model.BlogPostListItem{Slug: "just-published", UpdatedAt: updatedAt})
	updatedURLs := buildSitemapURLs("https://costrouter.ai", posts)
	require.Len(t, updatedURLs, 51)
	assert.Equal(t, "https://costrouter.ai/en/blog/just-published", updatedURLs[50].Loc)

	localized := buildSitemapURLs("https://costrouter.ai", []model.BlogPostListItem{
		{Slug: "shared", Locale: "en"},
		{Slug: "shared", Locale: "fr"},
		{Slug: "shared", Locale: "ja"},
		{Slug: "hidden", Locale: "zh"},
		{Slug: "invalid", Locale: "unsupported"},
	})
	require.Len(t, localized, 51)
	assert.Equal(t, "https://costrouter.ai/en/blog/shared", localized[48].Loc)
	assert.Equal(t, "https://costrouter.ai/fr/blog/shared", localized[49].Loc)
	assert.Equal(t, "https://costrouter.ai/ja/blog/shared", localized[50].Loc)
}

func TestSitemapProductionHostCanonicalization(t *testing.T) {
	for _, test := range []struct {
		host, forwardedHost, proto, want string
	}{
		{host: "www.costrouter.ai", want: "https://costrouter.ai"},
		{host: "costrouter.ai", proto: "http", want: "https://costrouter.ai"},
		{host: "127.0.0.1:3000", forwardedHost: "www.costrouter.ai, internal", proto: "https, http", want: "https://costrouter.ai"},
		{host: "localhost:3000", want: "http://localhost:3000"},
		{host: "test.example.com", proto: "https", want: "https://test.example.com"},
	} {
		t.Run(test.host+"/"+test.forwardedHost, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/sitemap.xml", nil)
			context.Request.Host = test.host
			context.Request.Header.Set("X-Forwarded-Host", test.forwardedHost)
			context.Request.Header.Set("X-Forwarded-Proto", test.proto)
			assert.Equal(t, test.want, publicSiteBaseURL(context))
		})
	}
}
