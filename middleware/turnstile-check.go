package middleware

import (
	"net/http"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if VerifyTurnstile(c) {
			c.Next()
		}
	}
}

// VerifyTurnstile validates the challenge without advancing the handler chain.
func VerifyTurnstile(c *gin.Context) bool {
	if common.TurnstileCheckEnabled {
		response := c.Query("turnstile")
		if response == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Turnstile token is required",
			})
			c.Abort()
			return false
		}
		rawRes, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
			"secret":   {common.TurnstileSecretKey},
			"response": {response},
			"remoteip": {c.ClientIP()},
		})
		if err != nil {
			common.SysLog(err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			c.Abort()
			return false
		}
		defer rawRes.Body.Close()
		var res turnstileCheckResponse
		err = common.DecodeJson(rawRes.Body, &res)
		if err != nil {
			common.SysLog(err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			c.Abort()
			return false
		}
		if !res.Success {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Turnstile verification failed. Please refresh and try again.",
			})
			c.Abort()
			return false
		}
	}
	return true
}
