package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHotModelCatalogUsesChannelModelsAndPreservesDates(t *testing.T) {
	resetPricingEndpointTestTables(t)
	require.NoError(t, DB.Exec("DELETE FROM hot_models").Error)
	t.Cleanup(func() { require.NoError(t, DB.Exec("DELETE FROM hot_models").Error) })

	channel := &Channel{Id: 801, Models: "old-model,vendor/new-model", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, channel.AddAbilities(nil))
	channel.Id = 802
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, DB.Model(&HotModel{}).Where("model_name = ?", "old-model").Update("created_time", 100).Error)
	require.NoError(t, DB.Model(&HotModel{}).Where("model_name = ?", "vendor/new-model").Update("created_time", 200).Error)

	var metadataCount int64
	require.NoError(t, DB.Model(&Model{}).Count(&metadataCount).Error)
	assert.Zero(t, metadataCount)
	createdTime := int64(300)
	require.NoError(t, SetHotModel("old-model", true, &createdTime))
	rows, err := GetHotModels()
	require.NoError(t, err)
	assert.Equal(t, []HotModel{{ModelName: "old-model", IsHot: true, CreatedTime: 300}, {ModelName: "vendor/new-model", CreatedTime: 200}}, rows)

	channel.Models += ",newly-added"
	require.NoError(t, channel.UpdateAbilities(nil))
	var added HotModel
	require.NoError(t, DB.First(&added, "model_name = ?", "newly-added").Error)
	assert.Positive(t, added.CreatedTime)
	var existing HotModel
	require.NoError(t, DB.First(&existing, "model_name = ?", "old-model").Error)
	assert.Equal(t, int64(300), existing.CreatedTime)
	assert.True(t, existing.IsHot)

	pricing := GetPricing()
	var found bool
	for _, row := range pricing {
		if row.ModelName == "old-model" {
			found = true
			assert.True(t, row.IsHot)
			assert.Equal(t, int64(300), row.CreatedTime)
		}
	}
	assert.True(t, found)
	require.NoError(t, SetHotModel("old-model", false, nil))
	require.NoError(t, DB.First(&existing, "model_name = ?", "old-model").Error)
	assert.False(t, existing.IsHot)
	assert.Equal(t, int64(300), existing.CreatedTime)
	assert.ErrorIs(t, SetHotModel("price-only-model", true, nil), ErrCatalogModelUnavailable)
	require.NoError(t, UpdateAbilityStatus(801, false))
	require.NoError(t, UpdateAbilityStatus(802, false))
	rows, err = GetHotModels()
	require.NoError(t, err)
	assert.Empty(t, rows)
	require.NoError(t, UpdateAbilityStatus(802, true))
	require.NoError(t, DB.First(&existing, "model_name = ?", "old-model").Error)
	assert.Equal(t, int64(300), existing.CreatedTime)
}

func TestHotModelMigrationPreservesLegacyFlagAndUnknownHistoricalDate(t *testing.T) {
	resetPricingEndpointTestTables(t)
	require.NoError(t, DB.Exec("DELETE FROM hot_models").Error)
	t.Cleanup(func() { require.NoError(t, DB.Exec("DELETE FROM hot_models").Error) })
	if !DB.Migrator().HasColumn(&Model{}, "is_hot") {
		require.NoError(t, DB.Exec("ALTER TABLE models ADD COLUMN is_hot BOOLEAN").Error)
	}
	meta := Model{ModelName: "legacy", Status: 1, CreatedTime: 999}
	require.NoError(t, DB.Create(&meta).Error)
	require.NoError(t, DB.Model(&Model{}).Where("id = ?", meta.Id).Update("is_hot", true).Error)
	insertPricingEndpointAbility(t, 801, "legacy")
	insertPricingEndpointAbility(t, 802, "without-metadata")
	require.NoError(t, migrateHotModels())
	rows, err := GetHotModels()
	require.NoError(t, err)
	assert.Equal(t, []HotModel{{ModelName: "legacy", IsHot: true}, {ModelName: "without-metadata"}}, rows)
	require.NoError(t, SetHotModel("legacy", false, nil))
	require.NoError(t, migrateHotModels())
	var row HotModel
	require.NoError(t, DB.First(&row, "model_name = ?", "legacy").Error)
	assert.False(t, row.IsHot)
	assert.Zero(t, row.CreatedTime)
}
