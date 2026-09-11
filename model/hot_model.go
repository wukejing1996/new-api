package model

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HotModel stores catalog preferences independently of optional model metadata.
type HotModel struct {
	ModelName   string `json:"model_name" gorm:"size:255;primaryKey"`
	IsHot       bool   `json:"is_hot"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

// Existing models have no reliable channel-entry timestamp. Keep it unknown (0).
// The legacy column is deliberately retained in the database for safe rollback.
func migrateHotModels() error {
	if err := DB.AutoMigrate(&HotModel{}); err != nil {
		return err
	}
	var names []string
	if err := DB.Model(&Ability{}).Distinct("model").Pluck("model", &names).Error; err != nil {
		return err
	}
	legacyHot := make(map[string]bool)
	if DB.Migrator().HasColumn(&Model{}, "is_hot") {
		var legacy []struct {
			ModelName string
			IsHot     bool
		}
		if err := DB.Model(&Model{}).Select("model_name", "is_hot").Where("name_rule = ?", NameRuleExact).Scan(&legacy).Error; err != nil {
			return err
		}
		for _, item := range legacy {
			legacyHot[item.ModelName] = item.IsHot
		}
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		row := HotModel{ModelName: name, IsHot: legacyHot[name]}
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// Called by channel writes in the same transaction. Existing dates and flags
// survive channel edits, duplicate channels, ability rebuilds and re-enabling.
func recordCatalogModels(tx *gorm.DB, names []string) error {
	rows := make([]HotModel, 0, len(names))
	seen := make(map[string]bool)
	for _, name := range names {
		if strings.TrimSpace(name) == "" || seen[name] {
			continue
		}
		seen[name] = true
		rows = append(rows, HotModel{ModelName: name, CreatedTime: common.GetTimestamp()})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 50).Error
}

func GetHotModels() ([]HotModel, error) {
	var rows []HotModel
	err := DB.Table("abilities").
		Select("DISTINCT abilities.model AS model_name, COALESCE(hot_models.is_hot, ?) AS is_hot, COALESCE(hot_models.created_time, 0) AS created_time", false).
		Joins("LEFT JOIN hot_models ON hot_models.model_name = abilities.model").
		Where("abilities.enabled = ? AND abilities.model <> ?", true, "").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []HotModel{}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsHot != rows[j].IsHot {
			return rows[i].IsHot
		}
		if rows[i].CreatedTime != rows[j].CreatedTime {
			return rows[i].CreatedTime > rows[j].CreatedTime
		}
		return rows[i].ModelName < rows[j].ModelName
	})
	return rows, nil
}

var ErrCatalogModelUnavailable = errors.New("model is not available in enabled channels")

func SetHotModel(name string, isHot bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Ability{}).Where("model = ? AND enabled = ?", name, true).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrCatalogModelUnavailable
		}
		row := HotModel{ModelName: name, IsHot: isHot}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "model_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"is_hot"}),
		}).Create(&row).Error
	})
}

func applyHotModelPreferences(pricing []Pricing) {
	var rows []HotModel
	if err := DB.Find(&rows).Error; err != nil {
		common.SysError("load hot model preferences: " + err.Error())
		return
	}
	byName := make(map[string]HotModel, len(rows))
	for _, row := range rows {
		byName[row.ModelName] = row
	}
	for i := range pricing {
		row := byName[pricing[i].ModelName]
		pricing[i].IsHot = row.IsHot
		pricing[i].CreatedTime = row.CreatedTime
	}
}
