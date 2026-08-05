package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerProxyRoutes(apiRouter *gin.RouterGroup) {
	r := apiRouter.Group("/proxy")
	r.Use(middleware.AdminAuth())
	// Static paths before /:id
	r.GET("/", controller.GetAllProxies)
	r.GET("/all", controller.GetAllProxiesNoPage)
	r.GET("/data", controller.ExportProxies)
	r.POST("/data", controller.ImportProxies)
	r.POST("/batch", controller.BatchCreateProxies)
	r.POST("/batch-delete", controller.BatchDeleteProxies)
	r.POST("/", controller.CreateProxy)
	r.GET("/:id", controller.GetProxy)
	r.PUT("/:id", controller.UpdateProxy)
	r.DELETE("/:id", controller.DeleteProxy)
	r.POST("/:id/test", controller.TestProxy)
	r.POST("/:id/quality-check", controller.CheckProxyQuality)
	r.GET("/:id/channels", controller.GetProxyChannels)
}
