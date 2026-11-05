package gateway

import (
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/handler"
	"github.com/julienschmidt/httprouter"
)

// RegisterManagementRoutes mendaftarkan semua rute untuk API Manajemen.
func RegisterManagementRoutes(router *httprouter.Router, manager *config.Manager) {
	h := &handler.ManagementHandler{
		Manager:   manager,
		StartTime: time.Now(),
		GetCircuitBreakerStats: func(serviceId string) (string, int, time.Time) {
			return GetCircuitBreaker(serviceId).Stats()
		},
		ResetCircuitBreaker: func(serviceId string) {
			GetCircuitBreaker(serviceId).Reset()
		},
		ResetMetrics: func() {
			DefaultMetrics.Reset()
		},
	}

	// === Gateway Status, Health & Full Config Routes ===
	router.GET("/api/status", h.GetGatewayStatus)
	router.GET("/api/health", h.GetGatewayStatus)
	router.GET("/api/config", h.GetFullConfig)

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

	// === CORS Config Routes ===
	router.GET("/api/config/cors", h.GetCORSConfig)
	router.POST("/api/config/cors", h.AddCORSConfig)
	router.PUT("/api/config/cors", h.UpdateCORSConfig)
	router.DELETE("/api/config/cors", h.DeleteCORSConfig)
	router.GET("/api/services/:serviceId/cors", h.GetServiceCORSConfig)
	router.POST("/api/services/:serviceId/cors", h.AddServiceCORSConfig)
	router.PUT("/api/services/:serviceId/cors", h.UpdateServiceCORSConfig)
	router.DELETE("/api/services/:serviceId/cors", h.DeleteServiceCORSConfig)
	router.GET("/api/routes/:routeId/cors", h.GetRouteCORSConfig)
	router.POST("/api/routes/:routeId/cors", h.AddRouteCORSConfig)
	router.PUT("/api/routes/:routeId/cors", h.UpdateRouteCORSConfig)
	router.DELETE("/api/routes/:routeId/cors", h.DeleteRouteCORSConfig)

	// === IP Restriction Config Routes ===
	router.GET("/api/config/ip-restriction", h.GetIPRestrictionConfig)
	router.POST("/api/config/ip-restriction", h.AddIPRestrictionConfig)
	router.PUT("/api/config/ip-restriction", h.UpdateIPRestrictionConfig)
	router.DELETE("/api/config/ip-restriction", h.DeleteIPRestrictionConfig)
	router.GET("/api/services/:serviceId/ip-restriction", h.GetServiceIPRestrictionConfig)
	router.POST("/api/services/:serviceId/ip-restriction", h.AddServiceIPRestrictionConfig)
	router.PUT("/api/services/:serviceId/ip-restriction", h.UpdateServiceIPRestrictionConfig)
	router.DELETE("/api/services/:serviceId/ip-restriction", h.DeleteServiceIPRestrictionConfig)
	router.GET("/api/routes/:routeId/ip-restriction", h.GetRouteIPRestrictionConfig)
	router.POST("/api/routes/:routeId/ip-restriction", h.AddRouteIPRestrictionConfig)
	router.PUT("/api/routes/:routeId/ip-restriction", h.UpdateRouteIPRestrictionConfig)
	router.DELETE("/api/routes/:routeId/ip-restriction", h.DeleteRouteIPRestrictionConfig)

	// === Request Transform Config Routes ===
	router.GET("/api/config/request-transform", h.GetRequestTransformConfig)
	router.POST("/api/config/request-transform", h.AddRequestTransformConfig)
	router.PUT("/api/config/request-transform", h.UpdateRequestTransformConfig)
	router.DELETE("/api/config/request-transform", h.DeleteRequestTransformConfig)
	router.GET("/api/services/:serviceId/request-transform", h.GetServiceRequestTransformConfig)
	router.POST("/api/services/:serviceId/request-transform", h.AddServiceRequestTransformConfig)
	router.PUT("/api/services/:serviceId/request-transform", h.UpdateServiceRequestTransformConfig)
	router.DELETE("/api/services/:serviceId/request-transform", h.DeleteServiceRequestTransformConfig)
	router.GET("/api/routes/:routeId/request-transform", h.GetRouteRequestTransformConfig)
	router.POST("/api/routes/:routeId/request-transform", h.AddRouteRequestTransformConfig)
	router.PUT("/api/routes/:routeId/request-transform", h.UpdateRouteRequestTransformConfig)
	router.DELETE("/api/routes/:routeId/request-transform", h.DeleteRouteRequestTransformConfig)

	// === Service Routes ===
	router.GET("/api/services", h.GetServices)
	router.POST("/api/services", h.AddService)
	router.GET("/api/services/:serviceId", h.GetServiceById)
	router.PUT("/api/services/:serviceId", h.UpdateService)
	router.DELETE("/api/services/:serviceId", h.DeleteService)
	router.GET("/api/services/:serviceId/circuit-breaker", h.GetServiceCircuitBreaker)
	router.POST("/api/services/:serviceId/circuit-breaker/reset", h.ResetServiceCircuitBreaker)

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

	// === Metrics Routes (Prometheus & JSON) ===
	router.Handler("GET", "/metrics", DefaultMetrics.Handler())
	router.Handler("GET", "/api/metrics", DefaultMetrics.Handler())
	router.POST("/api/metrics/reset", h.ResetGatewayMetrics)
}
