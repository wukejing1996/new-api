package model

import (
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	BlogPostStatusDraft     = "draft"
	BlogPostStatusPublished = "published"
)

var (
	blogSlugPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	blogUnsafeTagPattern = regexp.MustCompile(`(?is)<\s*(script|style|iframe|object|embed|form|meta|link)[^>]*>.*?<\s*/\s*(script|style|iframe|object|embed|form|meta|link)\s*>`)
	blogUnsafeSingleTag  = regexp.MustCompile(`(?is)<\s*(script|style|iframe|object|embed|form|meta|link)[^>]*\/?\s*>`)
	blogEventAttrPattern = regexp.MustCompile(`(?is)\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	blogJSHrefPattern    = regexp.MustCompile(`(?is)\s+(href|src)\s*=\s*("|\')\s*javascript:[^"\']*("|\')`)
	blogDataHrefPattern  = regexp.MustCompile(`(?is)\s+(href|src)\s*=\s*("|\')\s*data:text/html[^"\']*("|\')`)
	blogAllowedLocale    = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z]{2,4})?$`)
)

type BlogPost struct {
	Id int `json:"id"`

	Locale string `json:"locale" gorm:"type:varchar(16);not null;uniqueIndex:idx_blog_locale_slug,priority:1;index"`
	Slug   string `json:"slug" gorm:"type:varchar(160);not null;uniqueIndex:idx_blog_locale_slug,priority:2"`

	Title       string `json:"title" gorm:"type:varchar(255);not null"`
	Excerpt     string `json:"excerpt" gorm:"type:text"`
	ContentHTML string `json:"content_html" gorm:"type:text"`
	ContentJSON string `json:"content_json" gorm:"type:text"`
	CoverImage  string `json:"cover_image" gorm:"type:varchar(512)"`

	Status      string `json:"status" gorm:"type:varchar(32);not null;default:'draft';index"`
	PublishedAt int64  `json:"published_at" gorm:"bigint;index"`
	AuthorId    int    `json:"author_id" gorm:"index"`

	SEOTitle       string `json:"seo_title" gorm:"type:varchar(255)"`
	SEODescription string `json:"seo_description" gorm:"type:varchar(320)"`
	CanonicalURL   string `json:"canonical_url" gorm:"type:varchar(512)"`
	OGImage        string `json:"og_image" gorm:"type:varchar(512)"`
	Keywords       string `json:"keywords" gorm:"type:varchar(512)"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint;index"`
}

type BlogPostListItem struct {
	Id int `json:"id"`

	Locale string `json:"locale"`
	Slug   string `json:"slug"`

	Title       string `json:"title"`
	Excerpt     string `json:"excerpt"`
	CoverImage  string `json:"cover_image"`
	Status      string `json:"status"`
	PublishedAt int64  `json:"published_at"`
	AuthorId    int    `json:"author_id"`

	SEOTitle       string `json:"seo_title"`
	SEODescription string `json:"seo_description"`
	CanonicalURL   string `json:"canonical_url"`
	OGImage        string `json:"og_image"`
	Keywords       string `json:"keywords"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

type AdminBlogPostQuery struct {
	Locale  string
	Status  string
	Keyword string
	Offset  int
	Limit   int
}

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
	post.Locale = strings.TrimSpace(post.Locale)
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

	if post.Locale == "" || !blogAllowedLocale.MatchString(post.Locale) {
		return errors.New("article locale is invalid")
	}
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

func GetPublishedBlogPosts(locale string, offset int, limit int) ([]BlogPostListItem, int64, error) {
	var posts []BlogPostListItem
	query := DB.Model(&BlogPost{}).Where("status = ?", BlogPostStatusPublished)
	locale = strings.TrimSpace(locale)
	if locale != "" {
		query = query.Where("locale = ?", locale)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	err := query.Select(blogPostListSelect()).
		Offset(offset).
		Limit(limit).
		Order("published_at desc, id desc").
		Find(&posts).Error
	return posts, total, err
}

func GetPublishedBlogPost(locale string, slug string) (*BlogPost, error) {
	var post BlogPost
	query := DB.Where("status = ? AND slug = ?", BlogPostStatusPublished, strings.TrimSpace(slug))
	locale = strings.TrimSpace(locale)
	if locale != "" {
		query = query.Where("locale = ?", locale)
	}
	if err := query.First(&post).Error; err != nil {
		return nil, err
	}
	return &post, nil
}

func AdminListBlogPosts(params AdminBlogPostQuery) ([]BlogPostListItem, int64, error) {
	var posts []BlogPostListItem
	query := DB.Model(&BlogPost{})
	if locale := strings.TrimSpace(params.Locale); locale != "" {
		query = query.Where("locale = ?", locale)
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
	err := query.Select(blogPostListSelect()).
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
	return &post, nil
}

func CreateBlogPost(post *BlogPost) error {
	return DB.Create(post).Error
}

func UpdateBlogPost(post *BlogPost) error {
	if post == nil || post.Id <= 0 {
		return errors.New("invalid article id")
	}
	update := map[string]interface{}{
		"locale":          post.Locale,
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
	return DB.Where("id = ?", id).Delete(&BlogPost{}).Error
}

func SetBlogPostPublished(id int, published bool) (*BlogPost, error) {
	post, err := GetBlogPostById(id)
	if err != nil {
		return nil, err
	}
	post.Status = BlogPostStatusDraft
	if published {
		post.Status = BlogPostStatusPublished
		if post.PublishedAt == 0 {
			post.PublishedAt = common.GetTimestamp()
		}
	}
	if err := DB.Save(post).Error; err != nil {
		return nil, err
	}
	return post, nil
}

func IsBlogPostSlugConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "constraint")
}

func blogPostListSelect() []string {
	return []string{"id", "locale", "slug", "title", "excerpt", "cover_image", "status", "published_at", "author_id", "seo_title", "seo_description", "canonical_url", "og_image", "keywords", "created_at", "updated_at"}
}
