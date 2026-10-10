package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetUserGroupRateLimitStats(c *gin.Context) {
	stats, err := service.GetUserGroupRateLimitStats(c.Request.Context())
	if err != nil {
		logger.LogWarn(c.Request.Context(), "user group rate limit stats query failed: "+err.Error())
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Failed to load rate limit counts"})
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, stats)
}
