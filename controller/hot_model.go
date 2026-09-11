package controller

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetHotModels(c *gin.Context) {
	rows, err := model.GetHotModels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func SetHotModel(c *gin.Context) {
	var request struct {
		ModelName   string `json:"model_name"`
		IsHot       *bool  `json:"is_hot"`
		CreatedTime *int64 `json:"created_time"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.IsHot == nil || strings.TrimSpace(request.ModelName) == "" || utf8.RuneCountInString(request.ModelName) > 255 || request.CreatedTime != nil && *request.CreatedTime < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model_name and boolean is_hot are required"})
		return
	}
	if err := model.SetHotModel(request.ModelName, *request.IsHot, request.CreatedTime); err != nil {
		if errors.Is(err, model.ErrCatalogModelUnavailable) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	model.InvalidatePricingCache()
	common.ApiSuccess(c, nil)
}
