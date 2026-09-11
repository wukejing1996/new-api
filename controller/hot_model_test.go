package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHotModelAPIValidatesAndSavesExplicitFalse(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.HotModel{}))
	require.NoError(t, db.Create(&model.Ability{Model: "vendor/model", Group: "default", ChannelId: 1, Enabled: true}).Error)
	router := gin.New()
	router.GET("/", GetHotModels)
	router.PUT("/", SetHotModel)
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"model_name":"vendor/model"}`, http.StatusBadRequest},
		{`{"model_name":"vendor/model","is_hot":"true"}`, http.StatusBadRequest},
		{`{"model_name":"","is_hot":true}`, http.StatusBadRequest},
		{`{"model_name":"missing","is_hot":true}`, http.StatusNotFound},
		{`{"model_name":"vendor/model","is_hot":true}`, http.StatusOK},
		{`{"model_name":"vendor/model","is_hot":false}`, http.StatusOK},
	} {
		t.Run(test.body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, test.status, response.Code)
		})
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	var result struct {
		Success bool             `json:"success"`
		Data    []model.HotModel `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success)
	assert.Equal(t, []model.HotModel{{ModelName: "vendor/model"}}, result.Data)
}
