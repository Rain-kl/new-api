package middleware

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	abortWithRelayMessage(c, statusCode, message, code...)
}

func abortWithRelayMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	errorCode := types.ErrorCode("")
	if len(code) > 0 {
		errorCode = code[0]
	}
	codeStr := string(errorCode)
	userId := c.GetInt("id")
	requestID := c.GetString(common.RequestIdKey)

	_, preparedPluginRoute := c.Get(pluginruntime.ContextKeyRouteRequest)
	if preparedPluginRoute && RespondTaskPluginError(c, &dto.TaskError{
		Code:       codeStr,
		Message:    message,
		StatusCode: statusCode,
	}) {
		c.Abort()
		logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
		return
	}

	if strings.HasPrefix(c.Request.URL.Path, "/v1/messages") {
		newAPIError := types.NewClaudeError(
			errors.New(message),
			errorCode,
			statusCode,
			types.ErrOptionWithClaudeRequestID(requestID),
		)
		c.JSON(statusCode, newAPIError.ToClaudeErrorResponse())
	} else {
		c.JSON(statusCode, gin.H{
			"error": gin.H{
				"message": common.MessageWithRequestId(message, requestID),
				"type":    "new_api_error",
				"code":    codeStr,
			},
		})
	}
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
