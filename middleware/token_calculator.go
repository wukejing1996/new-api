package middleware

import "github.com/gin-gonic/gin"

func TokenCalculatorRateLimit() func(c *gin.Context) {
	return rateLimitFactory(30, 60, "TC")
}
