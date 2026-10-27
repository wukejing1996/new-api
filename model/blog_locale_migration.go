package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Add the required locale before AutoMigrate reconciles the composite index.
// MySQL DDL commits independently, so every step must be safe to repeat.
func migrateBlogLocales(db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasTable(&BlogPost{}) {
		return nil
	}
	if !migrator.HasColumn(&BlogPost{}, "Locale") {
		if err := migrator.AddColumn(&BlogPost{}, "Locale"); err != nil {
			return fmt.Errorf("add blog locale: %w", err)
		}
	}
	if err := db.Model(&BlogPost{}).Where("locale IS NULL OR locale = ?", "").UpdateColumn("locale", "en").Error; err != nil {
		return err
	}
	if !migrator.HasIndex(&BlogPost{}, "idx_blog_locale_slug") {
		if err := migrator.CreateIndex(&BlogPost{}, "idx_blog_locale_slug"); err != nil {
			return fmt.Errorf("create blog locale uniqueness: %w", err)
		}
	}
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		// A UNIQUE constraint owns its index and must be removed as a constraint.
		var constraints []string
		if err := db.Raw(`SELECT c.conname FROM pg_constraint c
JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
WHERE c.conrelid = ?::regclass AND c.contype = 'u'
AND array_length(c.conkey, 1) = 1 AND a.attname = 'slug'`, "blog_posts").Scan(&constraints).Error; err != nil {
			return err
		}
		for _, name := range constraints {
			if err := db.Exec("ALTER TABLE ? DROP CONSTRAINT ?", clause.Table{Name: "blog_posts"}, clause.Column{Name: name}).Error; err != nil {
				return err
			}
		}
	}
	indexes, err := migrator.GetIndexes(&BlogPost{})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, _ := index.Unique()
		columns := index.Columns()
		if unique && len(columns) == 1 && columns[0] == "slug" {
			if err := migrator.DropIndex(&BlogPost{}, index.Name()); err != nil {
				return fmt.Errorf("drop global blog slug index: %w", err)
			}
		}
	}
	return nil
}
