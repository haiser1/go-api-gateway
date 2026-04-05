package gateway

import (
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/handler"
	"github.com/julienschmidt/httprouter"
)

// RegisterManagementRoutes mendaftarkan semua rute untuk API Manajemen.
func RegisterManagementRoutes(router *httprouter.Router, manager *config.Manager) {
	h := &handler.ManagementHandler{
		Manager: manager,
	}

	// === Upstream Routes ===
	router.GET("/api/upstreams", h.GetUpstreams)
	router.POST("/api/upstreams", h.AddUpstream)
	router.GET("/api/upstreams/:upstreamId", h.GetUpstreamById)
	router.PUT("/api/upstreams/:upstreamId", h.UpdateUpstream)
	router.DELETE("/api/upstreams/:upstreamId", h.DeleteUpstream)

	// === Global Plugin Routes ===
	router.GET("/api/global-plugins", h.GetGlobalPlugins)
	router.POST("/api/global-plugins", h.AddGlobalPlugin)
	router.PUT("/api/global-plugins/:pluginName", h.UpdateGlobalPlugin)
	router.DELETE("/api/global-plugins/:pluginName", h.DeleteGlobalPlugin)

	// === Service Routes ===
	router.GET("/api/services", h.GetServices)
	router.POST("/api/services", h.AddService)
	router.GET("/api/services/:serviceId", h.GetServiceById)
	router.PUT("/api/services/:serviceId", h.UpdateService)
	router.DELETE("/api/services/:serviceId", h.DeleteService)

	// === Route Routes ===
	router.GET("/api/routes", h.GetRoutes)
	router.POST("/api/routes", h.AddRoute)
	router.GET("/api/routes/:routeId", h.GetRouteById)
	router.PUT("/api/routes/:routeId", h.UpdateRoute)
	router.DELETE("/api/routes/:routeId", h.DeleteRoute)

	// === Service Plugin Routes ===
	router.GET("/api/services/:serviceId/plugins", h.GetServicePlugins)
	router.POST("/api/services/:serviceId/plugins", h.AddPluginToService)
	router.PUT("/api/services/:serviceId/plugins/:pluginName", h.UpdatePluginInService)
	router.DELETE("/api/services/:serviceId/plugins/:pluginName", h.DeletePluginFromService)

	// === Route Plugin Routes ===
	router.GET("/api/routes/:routeId/plugins", h.GetRoutePlugins)
	router.POST("/api/routes/:routeId/plugins", h.AddPluginToRoute)
	router.PUT("/api/routes/:routeId/plugins/:pluginName", h.UpdatePluginInRoute)
	router.DELETE("/api/routes/:routeId/plugins/:pluginName", h.DeletePluginFromRoute)
}
