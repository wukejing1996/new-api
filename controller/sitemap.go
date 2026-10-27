package controller

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const sitemapDateFormat = "2006-01-02"

var sitemapLocaleSlugs = map[string]string{
	"en-US": "en",
	"fr-FR": "fr",
	"ja-JP": "ja",
	"ko-KR": "ko",
	"ru-RU": "ru",
	"vi-VN": "vi",
}

var sitemapStaticPaths = []string{
	"",
	"/models",
	"/tools/token-calculator",
	"/docs",
	"/docs/how-to-use",
	"/about",
	"/enterprise",
	"/blog",
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func GetSitemap(c *gin.Context) {
	posts, err := model.GetAllPublishedBlogPostsForSitemap()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to generate sitemap")
		return
	}

	urls := buildSitemapURLs(publicSiteBaseURL(c), posts)
	payload, err := xml.MarshalIndent(sitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}, "", "  ")
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to generate sitemap")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", append([]byte(xml.Header), payload...))
}

func buildSitemapURLs(baseURL string, posts []model.BlogPostListItem) []sitemapURL {
	urls := make([]sitemapURL, 0, len(sitemapLocaleSlugs)*len(sitemapStaticPaths)+len(posts))

	for _, locale := range []string{"en-US", "fr-FR", "ja-JP", "ko-KR", "ru-RU", "vi-VN"} {
		slug := sitemapLocaleSlugs[locale]
		for _, path := range sitemapStaticPaths {
			urls = append(urls, sitemapURL{
				Loc: baseURL + "/" + slug + path,
			})
		}
	}

	for _, post := range posts {
		if post.Slug == "" {
			continue
		}
		locale := post.Locale
		if locale == "" {
			locale = "en"
		}
		if locale == "zh" || !model.IsBlogLocale(locale) {
			continue
		}
		url := sitemapURL{
			Loc: baseURL + "/" + locale + "/blog/" + post.Slug,
		}
		if post.UpdatedAt > 0 {
			url.LastMod = time.Unix(post.UpdatedAt, 0).UTC().Format(sitemapDateFormat)
		}
		urls = append(urls, url)
	}

	return urls
}

func publicSiteBaseURL(c *gin.Context) string {
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	if commaIndex := strings.Index(proto, ","); commaIndex >= 0 {
		proto = strings.TrimSpace(proto[:commaIndex])
	}
	if proto != "http" && proto != "https" {
		proto = "https"
	}

	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if commaIndex := strings.Index(host, ","); commaIndex >= 0 {
		host = strings.TrimSpace(host[:commaIndex])
	}
	if host == "" {
		host = "costrouter.ai"
	}
	if strings.EqualFold(host, "costrouter.ai") || strings.EqualFold(host, "www.costrouter.ai") {
		return "https://costrouter.ai"
	}

	return proto + "://" + strings.TrimRight(host, "/")
}
