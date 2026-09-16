package controller

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func EstimateTokenCalculator(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "message": "text_limit"})
		return
	}
	var request service.CalculatorRequest
	if err := common.Unmarshal(body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_amount"})
		return
	}
	if err := request.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	userGroup := ""
	if id := c.GetInt("id"); id != 0 {
		user, err := model.GetUserCache(id)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "pricing_unavailable"})
			return
		}
		userGroup = user.Group
	}
	_, allowed := service.GetUserUsableGroups(userGroup)[request.Group]
	ratio, priced := ratio_setting.GetGroupRatioCopy()[request.Group]
	if !allowed || !priced || request.Group == "auto" || request.Group == "all" {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "group_unavailable"})
		return
	}
	if userGroup != "" {
		if special, ok := ratio_setting.GetGroupGroupRatio(userGroup, request.Group); ok {
			ratio = special
		}
	}
	var selected *model.Pricing
	for _, price := range model.GetPricing() {
		if price.ModelName == request.Model && (common.StringsContains(price.EnableGroup, request.Group) || common.StringsContains(price.EnableGroup, "all")) {
			selected = &price
			break
		}
	}
	if selected == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model_unavailable"})
		return
	}
	quote, err := service.EstimateCalculator(request, *selected, ratio)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": quote})
}
