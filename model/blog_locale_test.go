package model

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBlogLocaleValidation(t *testing.T) {
	for _, locale := range []string{"en", "fr", "ja", "ko", "ru", "vi", "zh", "", "en-US", "EN", "unknown"} {
		t.Run(locale, func(t *testing.T) {
			post := BlogPost{Locale: locale, Slug: "example", Title: "Example", ContentHTML: "<p>Content</p>"}
			if IsBlogLocale(locale) {
				assert.NoError(t, ValidateAndNormalizeBlogPost(&post))
			} else {
				assert.Error(t, ValidateAndNormalizeBlogPost(&post))
			}
		})
	}
	for _, test := range []struct {
		locales []string
		action  string
	}{
		{[]string{"fr"}, "publish"},
		{[]string{"en", "invalid"}, "publish"},
		{nil, "publish"},
		{[]string{"en"}, "invalid"},
	} {
		post := BlogPost{Locale: "en", Slug: "example", Title: "Example", ContentHTML: "<p>Content</p>"}
		assert.Error(t, SaveBlogPublication(&post, test.locales, test.action))
	}
}

// Opt-in only: these tests require dedicated disposable databases named with
// blog_locale_test. They never read application SQL_DSN or start a backend.
func TestBlogLocaleDatabaseMigrationAndPublication(t *testing.T) {
	if os.Getenv("TEST_BLOG_LOCALE_DB") != "1" {
		t.Skip("set TEST_BLOG_LOCALE_DB=1 and dedicated TEST_BLOG_MYSQL_DSN / TEST_BLOG_POSTGRES_DSN")
	}
	originalDB := DB
	mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() { DB = originalDB; common.SetDatabaseTypes(mainType, logType) })
	for _, engine := range []struct {
		name string
		kind common.DatabaseType
		dsn  string
	}{
		{"sqlite", common.DatabaseTypeSQLite, ""},
		{"mysql", common.DatabaseTypeMySQL, os.Getenv("TEST_BLOG_MYSQL_DSN")},
		{"postgres", common.DatabaseTypePostgreSQL, os.Getenv("TEST_BLOG_POSTGRES_DSN")},
	} {
		t.Run(engine.name, func(t *testing.T) {
			if engine.name != "sqlite" {
				require.Contains(t, engine.dsn, "blog_locale_test", "use a dedicated disposable test database")
			}
			for _, legacy := range []bool{false, true} {
				t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
					var dialector gorm.Dialector
					switch engine.name {
					case "mysql":
						dialector = mysql.Open(engine.dsn)
					case "postgres":
						dialector = postgres.Open(engine.dsn)
					default:
						dialector = sqlite.Open("file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared")
					}
					db, err := gorm.Open(dialector, &gorm.Config{})
					require.NoError(t, err)
					sqlDB, err := db.DB()
					require.NoError(t, err)
					t.Cleanup(func() { _ = sqlDB.Close() })
					DB = db
					common.SetDatabaseTypes(engine.kind, engine.kind)
					require.NoError(t, db.Migrator().DropTable(&BlogPost{}))
					t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&BlogPost{})) })
					if legacy {
						old := struct {
							Id          int
							Slug        string `gorm:"type:varchar(160);not null;uniqueIndex:idx_blog_posts_slug"`
							Title       string
							ContentHTML string
							Status      string
							ViewCount   int64
						}{Id: 41, Slug: "legacy", Title: "Preserved", ContentHTML: "<p>Preserved</p>", Status: "published", ViewCount: 321}
						require.NoError(t, db.Table("blog_posts").AutoMigrate(&old))
						require.NoError(t, db.Table("blog_posts").Create(&old).Error)
					}
					for range 2 {
						require.NoError(t, migrateBlogLocales(db))
						require.NoError(t, db.AutoMigrate(&BlogPost{}))
					}
					if legacy {
						var preserved BlogPost
						require.NoError(t, db.First(&preserved, 41).Error)
						assert.Equal(t, "en", preserved.Locale)
						assert.Equal(t, "Preserved", preserved.Title)
						assert.Equal(t, int64(321), preserved.ViewCount)
					}
					post := BlogPost{Locale: "en", Slug: "article", Title: "English", ContentHTML: "<p>English</p>", CanonicalURL: "/en/blog/article"}
					require.NoError(t, SaveBlogPublication(&post, []string{"en", "fr", "ja"}, "save"))
					require.Len(t, post.Versions, 3)
					_, total, err := GetPublishedBlogPosts(0, 10, "latest", "fr")
					require.NoError(t, err)
					assert.Zero(t, total)
					require.NoError(t, SaveBlogPublication(&post, []string{"en", "fr", "ja"}, "publish"))
					french, err := GetPublishedBlogPost(post.Slug, "fr")
					require.NoError(t, err)
					assert.Equal(t, "English", french.Title)
					assert.Empty(t, french.CanonicalURL, "translations default to their own canonical URL")
					french.Title = "French title"
					require.NoError(t, SaveBlogPublication(french, []string{"en", "fr"}, "save"))
					_, err = GetPublishedBlogPost(post.Slug, "ja")
					require.NoError(t, err, "save must not withdraw another version")
					require.NoError(t, SaveBlogPublication(&post, []string{"en"}, "publish"))
					_, err = GetPublishedBlogPost(post.Slug, "fr")
					require.ErrorIs(t, err, gorm.ErrRecordNotFound)
					require.NoError(t, SaveBlogPublication(&post, []string{"en", "fr"}, "publish"))
					restored, err := GetPublishedBlogPost(post.Slug, "fr")
					require.NoError(t, err)
					assert.Equal(t, french.Id, restored.Id)
					assert.Equal(t, "French title", restored.Title)
					originalViews := post.ViewCount
					_, err = IncrementPublishedBlogPostView(post.Slug, "fr")
					require.NoError(t, err)
					english, err := GetPublishedBlogPost(post.Slug, "en")
					require.NoError(t, err)
					assert.Equal(t, originalViews, english.ViewCount)
					duplicate := BlogPost{Slug: post.Slug, Locale: "fr", Title: "Duplicate"}
					assert.Error(t, db.Create(&duplicate).Error)
					other := BlogPost{Slug: "occupied", Locale: "en", Title: "Other", ContentHTML: "<p>Other</p>"}
					require.NoError(t, SaveBlogPublication(&other, []string{"en"}, "save"))
					post.Slug = "occupied"
					assert.ErrorIs(t, SaveBlogPublication(&post, []string{"en"}, "publish"), ErrBlogSlugConflict)
					post.Slug = "renamed"
					require.NoError(t, SaveBlogPublication(&post, []string{"en", "fr"}, "publish"))
					renamed, err := GetBlogPostById(french.Id)
					require.NoError(t, err)
					assert.Equal(t, "renamed", renamed.Slug)
					assert.Equal(t, "French title", renamed.Title)
					require.NoError(t, DeleteBlogPost(post.Id))
					var remaining int64
					require.NoError(t, db.Model(&BlogPost{}).Where("slug = ?", "renamed").Count(&remaining).Error)
					assert.Zero(t, remaining)
				})
			}
		})
	}
}
