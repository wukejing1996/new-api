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
	"/docs",
	"/docs/how-to-use",
	"/about",
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

	now := time.Now().UTC().Format(sitemapDateFormat)
	baseURL := publicSiteBaseURL(c)
	urls := make([]sitemapURL, 0, len(sitemapLocaleSlugs)*len(sitemapStaticPaths)+len(posts))

	for _, locale := range []string{"en-US", "fr-FR", "ja-JP", "ko-KR", "ru-RU", "vi-VN"} {
		slug := sitemapLocaleSlugs[locale]
		for _, path := range sitemapStaticPaths {
			urls = append(urls, sitemapURL{
				Loc:     baseURL + "/" + slug + path,
				LastMod: now,
			})
		}
	}

	for _, post := range posts {
		if post.Slug == "" {
			continue
		}
		url := sitemapURL{
			Loc: baseURL + "/blog/" + post.Slug,
		}
		if post.UpdatedAt > 0 {
			url.LastMod = time.Unix(post.UpdatedAt, 0).UTC().Format(sitemapDateFormat)
		}
		urls = append(urls, url)
	}

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

	return proto + "://" + strings.TrimRight(host, "/")
}
