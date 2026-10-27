package model

import (
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrBlogSlugConflict = errors.New("article slug already exists")

// SaveBlogPublication edits one version. Only publish applies the selected
// scope; saving content must not publish or withdraw other language versions.
func SaveBlogPublication(post *BlogPost, locales []string, action string) error {
	if err := ValidateAndNormalizeBlogPost(post); err != nil {
		return err
	}
	if action != "save" && action != "publish" && action != "unpublish" {
		return errors.New("invalid article action")
	}
	if locales != nil {
		if !slices.Contains(locales, "en") {
			return errors.New("English must be included in the publication scope")
		}
		for _, locale := range locales {
			if !IsBlogLocale(locale) {
				return errors.New("unsupported article locale")
			}
		}
	}
	if action == "publish" && locales == nil {
		return errors.New("publication scope is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var current BlogPost
		oldSlug := post.Slug
		creating := post.Id == 0
		if !creating {
			if err := tx.First(&current, post.Id).Error; err != nil {
				return err
			}
			oldSlug = current.Slug
			if current.Locale != post.Locale {
				return errors.New("an existing article version cannot change locale")
			}
			// All mutations lock the English version first, including edits to a
			// translation. This serializes scope changes and group-wide renames.
			var anchor BlogPost
			if err := lockForUpdate(tx).Where("slug = ? AND locale = ?", oldSlug, "en").First(&anchor).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).Where("id = ? AND slug = ?", post.Id, oldSlug).First(&current).Error; err != nil {
				return err
			}
		} else if post.Locale != "en" {
			return errors.New("new articles must start with English")
		}

		if creating || oldSlug != post.Slug {
			var count int64
			if err := tx.Model(&BlogPost{}).Where("slug = ?", post.Slug).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrBlogSlugConflict
			}
		}
		if !creating && oldSlug != post.Slug {
			if err := tx.Model(&BlogPost{}).Where("slug = ?", oldSlug).Updates(map[string]any{
				"slug": post.Slug, "updated_at": common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
		}

		post.Versions = nil
		if creating {
			post.Status = BlogPostStatusDraft
			post.PublishedAt = 0
			post.ViewCount = 0
			post.CreatedAt = 0
			if err := tx.Create(post).Error; err != nil {
				return err
			}
		} else {
			// Client-supplied status, timestamps, author and counters cannot alter
			// publication state as a side effect of saving the editor's content.
			post.Status = current.Status
			post.PublishedAt = current.PublishedAt
			post.AuthorId = current.AuthorId
			post.ViewCount = current.ViewCount
			post.CreatedAt = current.CreatedAt
			post.UpdatedAt = common.GetTimestamp()
			if err := tx.Model(&BlogPost{}).Where("id = ?", post.Id).Updates(map[string]any{
				"slug": post.Slug, "title": post.Title, "excerpt": post.Excerpt,
				"content_html": post.ContentHTML, "content_json": post.ContentJSON,
				"cover_image": post.CoverImage, "seo_title": post.SEOTitle,
				"seo_description": post.SEODescription, "canonical_url": post.CanonicalURL,
				"og_image": post.OGImage, "keywords": post.Keywords,
				"updated_at": post.UpdatedAt,
			}).Error; err != nil {
				return err
			}
		}

		var english BlogPost
		if err := lockForUpdate(tx).Where("slug = ? AND locale = ?", post.Slug, "en").First(&english).Error; err != nil {
			return err
		}
		var versions []BlogPost
		if err := lockForUpdate(tx).Where("slug = ?", post.Slug).Find(&versions).Error; err != nil {
			return err
		}
		for _, locale := range locales {
			if slices.ContainsFunc(versions, func(version BlogPost) bool { return version.Locale == locale }) {
				continue
			}
			version := english
			version.Id = 0
			version.Locale = locale
			version.Status = BlogPostStatusDraft
			version.PublishedAt = 0
			version.ViewCount = 0
			version.CreatedAt = 0
			version.Versions = nil
			// Each language defaults to its own URL, including after translation.
			version.CanonicalURL = ""
			if err := tx.Create(&version).Error; err != nil {
				return err
			}
			versions = append(versions, version)
		}
		for _, version := range versions {
			status := version.Status
			switch action {
			case "publish":
				status = BlogPostStatusDraft
				if slices.Contains(locales, version.Locale) {
					status = BlogPostStatusPublished
				}
			case "unpublish":
				if post.Locale == "en" || version.Id == post.Id {
					status = BlogPostStatusDraft
				}
			}
			if status == version.Status {
				continue
			}
			updates := map[string]any{"status": status, "updated_at": common.GetTimestamp()}
			if status == BlogPostStatusPublished {
				if version.PublishedAt == 0 {
					updates["published_at"] = common.GetTimestamp()
				}
				if version.ViewCount == 0 {
					updates["view_count"] = randomBlogInitialViewCount()
				}
			}
			if err := tx.Model(&BlogPost{}).Where("id = ?", version.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := tx.First(post, post.Id).Error; err != nil {
			return err
		}
		return tx.Model(&BlogPost{}).Select("id, locale, status").Where("slug = ?", post.Slug).Order("id asc").Find(&post.Versions).Error
	})
}
