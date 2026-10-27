package model

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	BlogPostStatusDraft     = "draft"
	BlogPostStatusPublished = "published"
	blogInitialViewCountMin = 250
	blogInitialViewCountMax = 400
)

var (
	blogSlugPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	blogUnsafeTagPattern = regexp.MustCompile(`(?is)<\s*(script|style|iframe|object|embed|form|meta|link)[^>]*>.*?<\s*/\s*(script|style|iframe|object|embed|form|meta|link)\s*>`)
	blogUnsafeSingleTag  = regexp.MustCompile(`(?is)<\s*(script|style|iframe|object|embed|form|meta|link)[^>]*\/?\s*>`)
	blogEventAttrPattern = regexp.MustCompile(`(?is)\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	blogJSHrefPattern    = regexp.MustCompile(`(?is)\s+(href|src)\s*=\s*("|\')\s*javascript:[^"\']*("|\')`)
	blogDataHrefPattern  = regexp.MustCompile(`(?is)\s+(href|src)\s*=\s*("|\')\s*data:text/html[^"\']*("|\')`)
)

type BlogPost struct {
	Id int `json:"id"`

	Slug   string `json:"slug" gorm:"type:varchar(160);not null;uniqueIndex:idx_blog_locale_slug,priority:2"`
	Locale string `json:"locale" gorm:"type:varchar(16);not null;default:en;uniqueIndex:idx_blog_locale_slug,priority:1;index:idx_blog_locale_status_published,priority:1"`

	Title       string `json:"title" gorm:"type:varchar(255);not null"`
	Excerpt     string `json:"excerpt" gorm:"type:text"`
	ContentHTML string `json:"content_html" gorm:"type:text"`
	ContentJSON string `json:"content_json" gorm:"type:text"`
	CoverImage  string `json:"cover_image" gorm:"type:text"`

	Status      string `json:"status" gorm:"type:varchar(32);not null;default:'draft';index;index:idx_blog_locale_status_published,priority:2"`
	PublishedAt int64  `json:"published_at" gorm:"bigint;index;index:idx_blog_locale_status_published,priority:3,sort:desc"`
	AuthorId    int    `json:"author_id" gorm:"index"`
	ViewCount   int64  `json:"view_count" gorm:"bigint;not null;default:0;index"`

	SEOTitle       string `json:"seo_title" gorm:"type:varchar(255)"`
	SEODescription string `json:"seo_description" gorm:"type:varchar(320)"`
	CanonicalURL   string `json:"canonical_url" gorm:"type:varchar(512)"`
	OGImage        string `json:"og_image" gorm:"type:text"`
	Keywords       string `json:"keywords" gorm:"type:varchar(512)"`

	CreatedAt int64             `json:"created_at" gorm:"bigint"`
	UpdatedAt int64             `json:"updated_at" gorm:"bigint;index"`
	Versions  []BlogPostVersion `json:"versions,omitempty" gorm:"-"`
}

type BlogPostVersion struct {
	Id     int    `json:"id"`
	Locale string `json:"locale"`
	Status string `json:"status"`
}

type BlogPostListItem struct {
	Id int `json:"id"`

	Slug   string `json:"slug"`
	Locale string `json:"locale"`

	Title         string `json:"title"`
	Excerpt       string `json:"excerpt"`
	CoverImage    string `json:"cover_image"`
	HasCoverImage bool   `json:"has_cover_image" gorm:"->"`
	Status        string `json:"status"`
	PublishedAt   int64  `json:"published_at"`
	AuthorId      int    `json:"author_id"`
	ViewCount     int64  `json:"view_count"`

	SEOTitle       string `json:"seo_title"`
	SEODescription string `json:"seo_description"`
	CanonicalURL   string `json:"canonical_url"`
	OGImage        string `json:"og_image"`
	Keywords       string `json:"keywords"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

type AdminBlogPostQuery struct {
	Status   string
	Keyword  string
	Offset   int
	Limit    int
	ListView bool
	Locale   string
}

type BlogPostSort string

const (
	BlogPostSortLatest        BlogPostSort = "latest"
	BlogPostSortPopularity    BlogPostSort = "popularity"
	BlogPostSortPublishedDesc BlogPostSort = "published_desc"
	BlogPostSortPublishedAsc  BlogPostSort = "published_asc"
	BlogPostSortViewsDesc     BlogPostSort = "views_desc"
)

func (p *BlogPost) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	return nil
}

func (p *BlogPost) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func ValidateAndNormalizeBlogPost(post *BlogPost) error {
	if post == nil {
		return errors.New("article is required")
	}
	if !IsBlogLocale(post.Locale) {
		return errors.New("article locale is required and must be supported")
	}
	post.Slug = strings.TrimSpace(strings.ToLower(post.Slug))
	post.Title = strings.TrimSpace(post.Title)
	post.Excerpt = strings.TrimSpace(post.Excerpt)
	post.CoverImage = strings.TrimSpace(post.CoverImage)
	post.SEOTitle = strings.TrimSpace(post.SEOTitle)
	post.SEODescription = strings.TrimSpace(post.SEODescription)
	post.CanonicalURL = strings.TrimSpace(post.CanonicalURL)
	post.OGImage = strings.TrimSpace(post.OGImage)
	post.Keywords = strings.TrimSpace(post.Keywords)
	post.Status = normalizeBlogStatus(post.Status)
	post.ContentHTML = sanitizeBlogHTML(post.ContentHTML)

	if post.Slug == "" || !blogSlugPattern.MatchString(post.Slug) {
		return errors.New("article slug must use lowercase letters, numbers, and hyphens")
	}
	if post.Title == "" {
		return errors.New("article title is required")
	}
	if post.ContentHTML == "" {
		return errors.New("article content is required")
	}
	if len(post.Slug) > 160 {
		return errors.New("article slug is too long")
	}
	if len(post.Title) > 255 {
		return errors.New("article title is too long")
	}
	if len(post.SEODescription) > 320 {
		return errors.New("SEO description is too long")
	}
	return nil
}

func normalizeBlogStatus(status string) string {
	switch strings.TrimSpace(status) {
	case BlogPostStatusPublished:
		return BlogPostStatusPublished
	default:
		return BlogPostStatusDraft
	}
}

func sanitizeBlogHTML(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = blogUnsafeTagPattern.ReplaceAllString(cleaned, "")
	cleaned = blogUnsafeSingleTag.ReplaceAllString(cleaned, "")
	cleaned = blogEventAttrPattern.ReplaceAllString(cleaned, "")
	cleaned = blogJSHrefPattern.ReplaceAllString(cleaned, "")
	cleaned = blogDataHrefPattern.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

func IsBlogLocale(locale string) bool {
	return slices.Contains([]string{"en", "zh", "fr", "ja", "ko", "ru", "vi"}, locale)
}

func GetPublishedBlogPosts(offset int, limit int, sort string, locale string) ([]BlogPostListItem, int64, error) {
	var posts []BlogPostListItem
	query := DB.Model(&BlogPost{}).Where("status = ? AND locale = ?", BlogPostStatusPublished, locale)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	err := query.Select(blogPostListSelect(false)).
		Offset(offset).
		Limit(limit).
		Order(blogPostOrderBy(sort)).
		Find(&posts).Error
	return posts, total, err
}

func GetAllPublishedBlogPostsForSitemap() ([]BlogPostListItem, error) {
	var posts []BlogPostListItem
	err := DB.Model(&BlogPost{}).
		Where("status = ?", BlogPostStatusPublished).
		Select([]string{"id", "slug", "locale", "updated_at"}).
		Order("published_at desc, id desc").
		Find(&posts).Error
	return posts, err
}

func GetPublishedBlogPost(slug string, locale string) (*BlogPost, error) {
	var post BlogPost
	query := DB.Where("status = ? AND slug = ? AND locale = ?", BlogPostStatusPublished, strings.TrimSpace(slug), locale)
	if err := query.First(&post).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&BlogPost{}).Select("id, locale, status").Where("slug = ? AND status = ?", post.Slug, BlogPostStatusPublished).Order("id asc").Find(&post.Versions).Error; err != nil {
		return nil, err
	}
	return &post, nil
}

func GetBlogPostCover(id int, publishedOnly bool) (string, error) {
	var post BlogPost
	query := DB.Model(&BlogPost{}).
		Select("cover_image").
		Where("id = ?", id)
	if publishedOnly {
		query = query.Where("status = ?", BlogPostStatusPublished)
	}
	if err := query.First(&post).Error; err != nil {
		return "", err
	}
	if strings.TrimSpace(post.CoverImage) == "" {
		return "", gorm.ErrRecordNotFound
	}
	return post.CoverImage, nil
}

func IncrementPublishedBlogPostView(slug string, locale string) (int64, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return 0, gorm.ErrRecordNotFound
	}

	query := DB.Model(&BlogPost{}).Where("status = ? AND slug = ? AND locale = ?", BlogPostStatusPublished, slug, locale)

	result := query.UpdateColumn("view_count", gorm.Expr("COALESCE(view_count, 0) + ?", 1))
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, gorm.ErrRecordNotFound
	}

	var post BlogPost
	lookup := DB.Model(&BlogPost{}).Select("view_count").Where("status = ? AND slug = ? AND locale = ?", BlogPostStatusPublished, slug, locale)
	if err := lookup.First(&post).Error; err != nil {
		return 0, err
	}
	return post.ViewCount, nil
}

func AdminListBlogPosts(params AdminBlogPostQuery) ([]BlogPostListItem, int64, error) {
	var posts []BlogPostListItem
	query := DB.Model(&BlogPost{})
	if params.Locale != "" {
		query = query.Where("locale = ?", params.Locale)
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		query = query.Where("status = ?", normalizeBlogStatus(status))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("title LIKE ? OR slug LIKE ? OR excerpt LIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	err := query.Select(blogPostListSelect(!params.ListView)).
		Offset(params.Offset).
		Limit(limit).
		Order("updated_at desc, id desc").
		Find(&posts).Error
	return posts, total, err
}

func GetBlogPostById(id int) (*BlogPost, error) {
	var post BlogPost
	if err := DB.Where("id = ?", id).First(&post).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&BlogPost{}).Select("id, locale, status").Where("slug = ?", post.Slug).Order("id asc").Find(&post.Versions).Error; err != nil {
		return nil, err
	}
	return &post, nil
}

func CreateBlogPost(post *BlogPost) error {
	if post != nil && post.Status == BlogPostStatusPublished {
		post.ViewCount = randomBlogInitialViewCount()
	}
	return DB.Create(post).Error
}

func UpdateBlogPost(post *BlogPost) error {
	if post == nil || post.Id <= 0 {
		return errors.New("invalid article id")
	}
	update := map[string]any{
		"slug":            post.Slug,
		"title":           post.Title,
		"excerpt":         post.Excerpt,
		"content_html":    post.ContentHTML,
		"content_json":    post.ContentJSON,
		"cover_image":     post.CoverImage,
		"status":          post.Status,
		"published_at":    post.PublishedAt,
		"seo_title":       post.SEOTitle,
		"seo_description": post.SEODescription,
		"canonical_url":   post.CanonicalURL,
		"og_image":        post.OGImage,
		"keywords":        post.Keywords,
		"updated_at":      common.GetTimestamp(),
	}
	if post.Status == BlogPostStatusPublished {
		var current BlogPost
		if err := DB.Select("view_count").Where("id = ?", post.Id).First(&current).Error; err != nil {
			return err
		}
		if current.ViewCount == 0 {
			update["view_count"] = randomBlogInitialViewCount()
		}
	}
	result := DB.Model(&BlogPost{}).Where("id = ?", post.Id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteBlogPost(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var post BlogPost
		if err := tx.First(&post, id).Error; err != nil {
			return err
		}
		var anchor BlogPost
		if err := lockForUpdate(tx).Where("slug = ? AND locale = ?", post.Slug, "en").First(&anchor).Error; err != nil {
			return err
		}
		query := tx.Where("id = ? AND slug = ?", id, post.Slug)
		if post.Locale == "en" {
			query = tx.Where("slug = ?", post.Slug)
		}
		return query.Delete(&BlogPost{}).Error
	})
}

func SetBlogPostPublished(id int, published bool) (*BlogPost, error) {
	post, err := GetBlogPostById(id)
	if err != nil {
		return nil, err
	}
	action := "unpublish"
	var locales []string
	if published {
		action = "publish"
		locales = []string{"en", post.Locale}
		for _, version := range post.Versions {
			if version.Status == BlogPostStatusPublished {
				locales = append(locales, version.Locale)
			}
		}
	}
	if err := SaveBlogPublication(post, locales, action); err != nil {
		return nil, err
	}
	return post, nil
}

func randomBlogInitialViewCount() int64 {
	return int64(blogInitialViewCountMin + common.GetRandomInt(blogInitialViewCountMax-blogInitialViewCountMin+1))
}

func IsBlogPostSlugConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "constraint")
}

func blogPostListSelect(includeCoverImage bool) []string {
	fields := []string{"id", "slug", "locale", "title", "excerpt", "CASE WHEN cover_image IS NOT NULL AND cover_image <> '' THEN TRUE ELSE FALSE END AS has_cover_image", "status", "published_at", "author_id", "view_count", "created_at", "updated_at"}
	if includeCoverImage {
		fields = append(fields, "cover_image", "seo_title", "seo_description", "canonical_url", "og_image", "keywords")
	}
	return fields
}

func blogPostOrderBy(sort string) string {
	switch BlogPostSort(strings.TrimSpace(sort)) {
	case BlogPostSortPublishedAsc:
		return "published_at asc, id asc"
	case BlogPostSortViewsDesc, BlogPostSortPopularity:
		return "view_count desc, published_at desc, id desc"
	default:
		return "published_at desc, id desc"
	}
}
