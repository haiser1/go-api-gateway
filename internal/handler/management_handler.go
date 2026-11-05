package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/domain"
	"github.com/haiser1/go-api-gateway/internal/helper"
	"github.com/julienschmidt/httprouter"
)

// ManagementHandler menampung logika untuk API manajemen
type ManagementHandler struct {
	Manager                *config.Manager
	StartTime              time.Time
	GetCircuitBreakerStats func(serviceId string) (state string, failures int, lastFailure time.Time)
	ResetCircuitBreaker    func(serviceId string)
	ResetMetrics           func()
}

// --- HELPER UNTUK KONVERSI DTO ---

func dtoToConfigUpstream(dto domain.AddUpstreamRequest) config.Upstream {
	targets := make([]config.UpstreamTarget, len(dto.Targets))
	for i, t := range dto.Targets {
		targets[i] = config.UpstreamTarget{
			Host:   t.Host,
			Port:   t.Port,
			Weight: t.Weight,
		}
		if t.HealthCheck != nil {
			targets[i].HealthCheck = &config.HealthCheckConfig{
				Path:     t.HealthCheck.Path,
				Interval: t.HealthCheck.Interval,
			}
		}
	}

	u := config.Upstream{
		Id:        uuid.NewString(),
		Name:      dto.Name,
		Algorithm: dto.Algorithm,
		Targets:   targets,
	}
	u.ApplyDefaults()
	return u
}

func dtoToConfigService(dto domain.AddServiceRequest) config.Service {
	plugins := make([]config.PluginConfig, len(dto.Plugins))
	for i, p := range dto.Plugins {
		plugins[i] = config.PluginConfig{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
	}

	svc := config.Service{
		Id:             uuid.NewString(),
		Name:           dto.Name,
		UpstreamId:     dto.UpstreamId,
		Protocol:       dto.Protocol,
		Plugins:        plugins,
		Timeout:        dto.Timeout,
		ConnectTimeout: dto.ConnectTimeout,
		ReadTimeout:    dto.ReadTimeout,
		Retries:        dto.Retries,
		RetryBackoff:   dto.RetryBackoff,
	}
	svc.ApplyDefaults()
	return svc
}

func dtoToConfigRoute(dto domain.AddRouteRequest) config.Route {
	plugins := make([]config.PluginConfig, len(dto.Plugins))
	for i, p := range dto.Plugins {
		plugins[i] = config.PluginConfig{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
	}
	return config.Route{
		Id:          uuid.NewString(),
		Name:        dto.Name,
		Methods:     dto.Methods,
		Paths:       dto.Paths,
		ServiceId:   dto.ServiceId,
		StripPrefix: dto.StripPrefix,
		Plugins:     plugins,
	}
}

func dtoToConfigPlugin(dto domain.Plugin) config.PluginConfig {
	return config.PluginConfig{
		Name:    dto.Name,
		Enabled: dto.Enabled,
		Config:  dto.Config,
	}
}

// --- UPSTREAM HANDLERS ---

func (h *ManagementHandler) GetUpstreams(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	helper.RespondSuccess(w, http.StatusOK, "Upstreams fetched successfully", cfg.Upstreams)
}

func (h *ManagementHandler) GetUpstreamById(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	upstreamId := ps.ByName("upstreamId")
	cfg := h.Manager.GetConfig()

	for _, u := range cfg.Upstreams {
		if u.Id == upstreamId {
			// Convert to response DTO
			targets := make([]domain.UpstreamTargetDTO, len(u.Targets))
			for i, t := range u.Targets {
				targets[i] = domain.UpstreamTargetDTO{
					Host:   t.Host,
					Port:   t.Port,
					Weight: t.Weight,
				}
				if t.HealthCheck != nil {
					targets[i].HealthCheck = &domain.HealthCheckDTO{
						Path:     t.HealthCheck.Path,
						Interval: t.HealthCheck.Interval,
					}
				}
			}
			response := domain.UpstreamDetailResponse{
				Id:        u.Id,
				Name:      u.Name,
				Algorithm: u.Algorithm,
				Targets:   targets,
			}
			helper.RespondSuccess(w, http.StatusOK, "Upstream fetched successfully", response)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Upstream not found", nil)
}

func (h *ManagementHandler) AddUpstream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.AddUpstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" || len(req.Targets) == 0 {
		helper.RespondError(w, http.StatusBadRequest, "Missing required fields: name, targets", nil)
		return
	}
	newUpstream := dtoToConfigUpstream(req)
	if err := h.Manager.AddUpstream(newUpstream); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add upstream", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Upstream added successfully", newUpstream)
}

func (h *ManagementHandler) UpdateUpstream(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	upstreamId := ps.ByName("upstreamId")
	var req domain.UpdateUpstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	targets := make([]config.UpstreamTarget, len(req.Targets))
	for i, t := range req.Targets {
		targets[i] = config.UpstreamTarget{
			Host:   t.Host,
			Port:   t.Port,
			Weight: t.Weight,
		}
		if t.HealthCheck != nil {
			targets[i].HealthCheck = &config.HealthCheckConfig{
				Path:     t.HealthCheck.Path,
				Interval: t.HealthCheck.Interval,
			}
		}
	}

	updatedUpstream := config.Upstream{
		Id:        upstreamId,
		Name:      req.Name,
		Algorithm: req.Algorithm,
		Targets:   targets,
	}

	if err := h.Manager.UpdateUpstream(upstreamId, updatedUpstream); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update upstream", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Upstream updated successfully", updatedUpstream)
}

func (h *ManagementHandler) DeleteUpstream(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	upstreamId := ps.ByName("upstreamId")
	if err := h.Manager.DeleteUpstream(upstreamId); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete upstream", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Upstream deleted successfully", nil)
}

// --- GLOBAL PLUGIN HANDLERS ---

func (h *ManagementHandler) GetGlobalPlugins(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	helper.RespondSuccess(w, http.StatusOK, "Global plugins fetched successfully", cfg.GlobalPlugins)
}

func (h *ManagementHandler) AddGlobalPlugin(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.Plugin
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		helper.RespondError(w, http.StatusBadRequest, "Plugin name required", nil)
		return
	}

	plugin := dtoToConfigPlugin(req)
	if err := h.Manager.AddGlobalPlugin(plugin); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add global plugin", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Global plugin added successfully", plugin)
}

func (h *ManagementHandler) UpdateGlobalPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginName := ps.ByName("pluginName")
	var req domain.Plugin
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		req.Name = pluginName // Pastikan nama konsisten
	}

	plugin := dtoToConfigPlugin(req)
	if err := h.Manager.UpdateGlobalPlugin(pluginName, plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update global plugin", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Global plugin updated successfully", plugin)
}

func (h *ManagementHandler) DeleteGlobalPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginName := ps.ByName("pluginName")
	if err := h.Manager.DeleteGlobalPlugin(pluginName); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete global plugin", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Global plugin deleted successfully", nil)
}

// --- SERVICE HANDLERS ---

func (h *ManagementHandler) GetServices(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	helper.RespondSuccess(w, http.StatusOK, "Services fetched successfully", cfg.Services)
}

func (h *ManagementHandler) GetServiceById(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()

	var targetService config.Service
	serviceFound := false
	for _, s := range cfg.Services {
		if s.Id == serviceId {
			targetService = s
			serviceFound = true
			break
		}
	}
	if !serviceFound {
		helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
		return
	}

	matchingRoutes := make([]domain.RouteDetail, 0)
	for _, route := range cfg.Routes {
		if route.ServiceId == targetService.Id {
			routePlugins := make([]domain.Plugin, len(route.Plugins))
			for i, p := range route.Plugins {
				routePlugins[i] = domain.Plugin{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
			}
			routeDetail := domain.RouteDetail{
				Id:      route.Id,
				Name:    route.Name,
				Methods: route.Methods,
				Paths:   route.Paths,
				Plugins: routePlugins,
			}
			matchingRoutes = append(matchingRoutes, routeDetail)
		}
	}

	servicePlugins := make([]domain.Plugin, len(targetService.Plugins))
	for i, p := range targetService.Plugins {
		servicePlugins[i] = domain.Plugin{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
	}

	response := domain.ServiceDetailResponse{
		Id:             targetService.Id,
		Name:           targetService.Name,
		UpstreamId:     targetService.UpstreamId,
		Protocol:       targetService.Protocol,
		Plugins:        servicePlugins,
		Routes:         matchingRoutes,
		Timeout:        targetService.Timeout,
		ConnectTimeout: targetService.ConnectTimeout,
		ReadTimeout:    targetService.ReadTimeout,
		Retries:        targetService.Retries,
		RetryBackoff:   targetService.RetryBackoff,
	}

	helper.RespondSuccess(w, http.StatusOK, "Service fetched successfully", response)
}

func (h *ManagementHandler) AddService(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.AddServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" || req.UpstreamId == "" {
		helper.RespondError(w, http.StatusBadRequest, "Missing required fields: name, upstream_id", nil)
		return
	}
	newService := dtoToConfigService(req)
	if err := h.Manager.AddService(newService); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add service", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Service added successfully", newService)
}

func (h *ManagementHandler) UpdateService(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.UpdateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugins := make([]config.PluginConfig, len(req.Plugins))
	for i, p := range req.Plugins {
		plugins[i] = config.PluginConfig{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
	}

	updatedService := config.Service{
		Id:             serviceId,
		Name:           req.Name,
		UpstreamId:     req.UpstreamId,
		Protocol:       req.Protocol,
		Plugins:        plugins,
		Timeout:        req.Timeout,
		ConnectTimeout: req.ConnectTimeout,
		ReadTimeout:    req.ReadTimeout,
		Retries:        req.Retries,
		RetryBackoff:   req.RetryBackoff,
	}

	if err := h.Manager.UpdateService(serviceId, updatedService); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update service", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Service updated successfully", updatedService)
}

func (h *ManagementHandler) DeleteService(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	if err := h.Manager.DeleteService(serviceId); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete service", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Service deleted successfully", nil)
}

// --- ROUTE HANDLERS ---

func (h *ManagementHandler) GetRoutes(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	helper.RespondSuccess(w, http.StatusOK, "Routes fetched successfully", cfg.Routes)
}

func (h *ManagementHandler) GetRouteById(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	cfg := h.Manager.GetConfig()

	var targetRoute config.Route
	routeFound := false
	for _, rt := range cfg.Routes {
		if rt.Id == routeId {
			targetRoute = rt
			routeFound = true
			break
		}
	}
	if !routeFound {
		helper.RespondError(w, http.StatusNotFound, "Route not found", nil)
		return
	}

	var serviceSnapshot *domain.ServiceSnapshot
	for _, s := range cfg.Services {
		if s.Id == targetRoute.ServiceId {
			serviceSnapshot = &domain.ServiceSnapshot{
				Id:   s.Id,
				Name: s.Name,
			}
			break
		}
	}

	routePlugins := make([]domain.Plugin, len(targetRoute.Plugins))
	for i, p := range targetRoute.Plugins {
		routePlugins[i] = domain.Plugin{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
	}

	response := domain.RouteDetailResponse{
		Id:      targetRoute.Id,
		Name:    targetRoute.Name,
		Methods: targetRoute.Methods,
		Paths:   targetRoute.Paths,
		Plugins: routePlugins,
		Service: serviceSnapshot,
	}
	helper.RespondSuccess(w, http.StatusOK, "Route fetched successfully", response)
}

func (h *ManagementHandler) AddRoute(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.AddRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" || len(req.Paths) == 0 || len(req.Methods) == 0 || req.ServiceId == "" {
		helper.RespondError(w, http.StatusBadRequest, "Missing required fields: name, methods, paths, serviceId", nil)
		return
	}
	newRoute := dtoToConfigRoute(req)
	if err := h.Manager.AddRoute(newRoute); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Route added successfully", newRoute)
}

func (h *ManagementHandler) UpdateRoute(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.UpdateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" || len(req.Paths) == 0 || len(req.Methods) == 0 || req.ServiceId == "" {
		helper.RespondError(w, http.StatusBadRequest, "Missing required fields: name, methods, paths, serviceId", nil)
		return
	}

	plugins := make([]config.PluginConfig, len(req.Plugins))
	for i, p := range req.Plugins {
		plugins[i] = config.PluginConfig{Name: p.Name, Enabled: p.Enabled, Config: p.Config}
	}

	updatedRoute := config.Route{
		Id:          routeId,
		Name:        req.Name,
		Methods:     req.Methods,
		Paths:       req.Paths,
		ServiceId:   req.ServiceId,
		StripPrefix: req.StripPrefix,
		Plugins:     plugins,
	}

	if err := h.Manager.UpdateRoute(routeId, updatedRoute); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Route updated successfully", updatedRoute)
}

func (h *ManagementHandler) DeleteRoute(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	if err := h.Manager.DeleteRoute(routeId); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Route deleted successfully", nil)
}

// --- PLUGIN HANDLERS: SERVICE ---

func (h *ManagementHandler) GetServicePlugins(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()
	for _, s := range cfg.Services {
		if s.Id == serviceId {
			helper.RespondSuccess(w, http.StatusOK, "Service plugins fetched successfully", s.Plugins)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
}

func (h *ManagementHandler) AddPluginToService(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.Plugin
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		helper.RespondError(w, http.StatusBadRequest, "Plugin name required", nil)
		return
	}
	plugin := dtoToConfigPlugin(req)
	if err := h.Manager.AddPluginToService(serviceId, plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to add plugin", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Plugin added to service", plugin)
}

func (h *ManagementHandler) UpdatePluginInService(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	pluginName := ps.ByName("pluginName")
	var req domain.Plugin
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		req.Name = pluginName
	}
	plugin := dtoToConfigPlugin(req)
	if err := h.Manager.UpdatePluginInService(serviceId, pluginName, plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update plugin", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Plugin updated successfully", plugin)
}

func (h *ManagementHandler) DeletePluginFromService(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	pluginName := ps.ByName("pluginName")
	if err := h.Manager.DeletePluginFromService(serviceId, pluginName); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete plugin", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Plugin deleted successfully", nil)
}

// --- PLUGIN HANDLERS: ROUTE ---

func (h *ManagementHandler) GetRoutePlugins(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	cfg := h.Manager.GetConfig()
	for _, rt := range cfg.Routes {
		if rt.Id == routeId {
			helper.RespondSuccess(w, http.StatusOK, "Route plugins fetched successfully", rt.Plugins)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Route not found", nil)
}

func (h *ManagementHandler) AddPluginToRoute(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.Plugin
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		helper.RespondError(w, http.StatusBadRequest, "Plugin name required", nil)
		return
	}
	plugin := dtoToConfigPlugin(req)
	if err := h.Manager.AddPluginToRoute(routeId, plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to add plugin to route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Plugin added to route", plugin)
}

func (h *ManagementHandler) UpdatePluginInRoute(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	pluginName := ps.ByName("pluginName")
	var req domain.Plugin
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		req.Name = pluginName
	}
	plugin := dtoToConfigPlugin(req)
	if err := h.Manager.UpdatePluginInRoute(routeId, pluginName, plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update plugin in route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Plugin updated successfully", plugin)
}

func (h *ManagementHandler) DeletePluginFromRoute(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	pluginName := ps.ByName("pluginName")
	if err := h.Manager.DeletePluginFromRoute(routeId, pluginName); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete plugin from route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Plugin deleted successfully", nil)
}

// === CORS CONFIG HANDLERS ===

func parseCORSConfig(p config.PluginConfig) domain.CORSConfig {
	var allowedOrigins []string
	if origins, ok := p.Config["allowed_origins"].([]interface{}); ok {
		for _, o := range origins {
			if s, ok := o.(string); ok {
				allowedOrigins = append(allowedOrigins, s)
			}
		}
	} else if origins, ok := p.Config["allowed_origins"].([]string); ok {
		allowedOrigins = origins
	}

	var allowedMethods []string
	if methods, ok := p.Config["allowed_methods"].([]interface{}); ok {
		for _, m := range methods {
			if s, ok := m.(string); ok {
				allowedMethods = append(allowedMethods, s)
			}
		}
	} else if methods, ok := p.Config["allowed_methods"].([]string); ok {
		allowedMethods = methods
	}

	var allowedHeaders []string
	if headers, ok := p.Config["allowed_headers"].([]interface{}); ok {
		for _, h := range headers {
			if s, ok := h.(string); ok {
				allowedHeaders = append(allowedHeaders, s)
			}
		}
	} else if headers, ok := p.Config["allowed_headers"].([]string); ok {
		allowedHeaders = headers
	}

	var exposedHeaders []string
	if headers, ok := p.Config["exposed_headers"].([]interface{}); ok {
		for _, h := range headers {
			if s, ok := h.(string); ok {
				exposedHeaders = append(exposedHeaders, s)
			}
		}
	} else if headers, ok := p.Config["exposed_headers"].([]string); ok {
		exposedHeaders = headers
	}

	allowCredentials := false
	if creds, ok := p.Config["allow_credentials"].(bool); ok {
		allowCredentials = creds
	}

	maxAge := 0
	if ageFloat, ok := p.Config["max_age"].(float64); ok {
		maxAge = int(ageFloat)
	} else if ageInt, ok := p.Config["max_age"].(int); ok {
		maxAge = ageInt
	}

	return domain.CORSConfig{
		Enabled:          p.Enabled,
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   allowedMethods,
		AllowedHeaders:   allowedHeaders,
		ExposedHeaders:   exposedHeaders,
		AllowCredentials: allowCredentials,
		MaxAge:           maxAge,
	}
}

func corsConfigToPlugin(dto domain.CORSConfig) config.PluginConfig {
	return config.PluginConfig{
		Name:    "cors",
		Enabled: dto.Enabled,
		Config: map[string]interface{}{
			"allowed_origins":   dto.AllowedOrigins,
			"allowed_methods":   dto.AllowedMethods,
			"allowed_headers":   dto.AllowedHeaders,
			"exposed_headers":   dto.ExposedHeaders,
			"allow_credentials": dto.AllowCredentials,
			"max_age":           dto.MaxAge,
		},
	}
}

func (h *ManagementHandler) GetCORSConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	for _, p := range cfg.GlobalPlugins {
		if p.Name == "cors" {
			corsDTO := parseCORSConfig(p)
			helper.RespondSuccess(w, http.StatusOK, "CORS configuration fetched successfully", corsDTO)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "CORS configuration not found", nil)
}

func (h *ManagementHandler) AddCORSConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.CORSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugin := corsConfigToPlugin(req)
	if err := h.Manager.AddGlobalPlugin(plugin); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "CORS configuration added successfully", req)
}

func (h *ManagementHandler) UpdateCORSConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.CORSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugin := corsConfigToPlugin(req)
	if err := h.Manager.UpdateGlobalPlugin("cors", plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "CORS configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteCORSConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := h.Manager.DeleteGlobalPlugin("cors"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "CORS configuration deleted successfully", nil)
}

// --- Service-Level CORS Config Handlers ---

func (h *ManagementHandler) GetServiceCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()
	for _, s := range cfg.Services {
		if s.Id == serviceId {
			for _, p := range s.Plugins {
				if p.Name == "cors" {
					helper.RespondSuccess(w, http.StatusOK, "Service CORS configuration fetched successfully", parseCORSConfig(p))
					return
				}
			}
			helper.RespondError(w, http.StatusNotFound, "CORS configuration not found on service", nil)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
}

func (h *ManagementHandler) AddServiceCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.CORSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugin := corsConfigToPlugin(req)
	if err := h.Manager.AddPluginToService(serviceId, plugin); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add CORS configuration to service", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Service CORS configuration added successfully", req)
}

func (h *ManagementHandler) UpdateServiceCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.CORSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugin := corsConfigToPlugin(req)
	if err := h.Manager.UpdatePluginInService(serviceId, "cors", plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update service CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Service CORS configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteServiceCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	if err := h.Manager.DeletePluginFromService(serviceId, "cors"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete service CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Service CORS configuration deleted successfully", nil)
}

// --- Route-Level CORS Config Handlers ---

func (h *ManagementHandler) GetRouteCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	cfg := h.Manager.GetConfig()
	for _, rt := range cfg.Routes {
		if rt.Id == routeId {
			for _, p := range rt.Plugins {
				if p.Name == "cors" {
					helper.RespondSuccess(w, http.StatusOK, "Route CORS configuration fetched successfully", parseCORSConfig(p))
					return
				}
			}
			helper.RespondError(w, http.StatusNotFound, "CORS configuration not found on route", nil)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Route not found", nil)
}

func (h *ManagementHandler) AddRouteCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.CORSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugin := corsConfigToPlugin(req)
	if err := h.Manager.AddPluginToRoute(routeId, plugin); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add CORS configuration to route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Route CORS configuration added successfully", req)
}

func (h *ManagementHandler) UpdateRouteCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.CORSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	plugin := corsConfigToPlugin(req)
	if err := h.Manager.UpdatePluginInRoute(routeId, "cors", plugin); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update route CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Route CORS configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteRouteCORSConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	if err := h.Manager.DeletePluginFromRoute(routeId, "cors"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete route CORS configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Route CORS configuration deleted successfully", nil)
}

// === IP RESTRICTION CONFIG HANDLERS ===

func extractStringSlice(raw interface{}) []string {
	var res []string
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				res = append(res, s)
			}
		}
	case []string:
		res = append(res, v...)
	}
	return res
}

func extractStringMap(raw interface{}) map[string]string {
	switch v := raw.(type) {
	case map[string]interface{}:
		res := make(map[string]string, len(v))
		for k, val := range v {
			if s, ok := val.(string); ok {
				res[k] = s
			}
		}
		return res
	case map[string]string:
		res := make(map[string]string, len(v))
		for k, val := range v {
			res[k] = val
		}
		return res
	}
	return nil
}

func parseIPRestrictionConfig(p config.PluginConfig) domain.IPRestrictionConfig {
	allowed := extractStringSlice(p.Config["allowed_ips"])
	if len(allowed) == 0 {
		allowed = extractStringSlice(p.Config["whitelist"])
	}
	denied := extractStringSlice(p.Config["denied_ips"])
	if len(denied) == 0 {
		denied = extractStringSlice(p.Config["blacklist"])
	}
	bots := extractStringSlice(p.Config["blocked_bots"])
	if len(bots) == 0 {
		bots = extractStringSlice(p.Config["denied_user_agents"])
	}
	trustFF, _ := p.Config["trust_forwarded_for"].(bool)

	statusCode := 0
	if scFloat, ok := p.Config["status_code"].(float64); ok {
		statusCode = int(scFloat)
	} else if scInt, ok := p.Config["status_code"].(int); ok {
		statusCode = scInt
	}
	msg, _ := p.Config["message"].(string)

	return domain.IPRestrictionConfig{
		Enabled:           p.Enabled,
		AllowedIPs:        allowed,
		DeniedIPs:         denied,
		BlockedBots:       bots,
		TrustForwardedFor: trustFF,
		StatusCode:        statusCode,
		Message:           msg,
	}
}

func ipRestrictionToPlugin(dto domain.IPRestrictionConfig) config.PluginConfig {
	cfg := map[string]interface{}{
		"allowed_ips":         dto.AllowedIPs,
		"denied_ips":          dto.DeniedIPs,
		"blocked_bots":        dto.BlockedBots,
		"trust_forwarded_for": dto.TrustForwardedFor,
	}
	if dto.StatusCode > 0 {
		cfg["status_code"] = dto.StatusCode
	}
	if dto.Message != "" {
		cfg["message"] = dto.Message
	}
	return config.PluginConfig{
		Name:    "ip-restriction",
		Enabled: dto.Enabled,
		Config:  cfg,
	}
}

func (h *ManagementHandler) GetIPRestrictionConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	for _, p := range cfg.GlobalPlugins {
		if p.Name == "ip-restriction" {
			helper.RespondSuccess(w, http.StatusOK, "IP restriction configuration fetched successfully", parseIPRestrictionConfig(p))
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "IP restriction configuration not found", nil)
}

func (h *ManagementHandler) AddIPRestrictionConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.IPRestrictionConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.AddGlobalPlugin(ipRestrictionToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "IP restriction configuration added successfully", req)
}

func (h *ManagementHandler) UpdateIPRestrictionConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.IPRestrictionConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.UpdateGlobalPlugin("ip-restriction", ipRestrictionToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "IP restriction configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteIPRestrictionConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := h.Manager.DeleteGlobalPlugin("ip-restriction"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "IP restriction configuration deleted successfully", nil)
}

func (h *ManagementHandler) GetServiceIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()
	for _, s := range cfg.Services {
		if s.Id == serviceId {
			for _, p := range s.Plugins {
				if p.Name == "ip-restriction" {
					helper.RespondSuccess(w, http.StatusOK, "Service IP restriction configuration fetched successfully", parseIPRestrictionConfig(p))
					return
				}
			}
			helper.RespondError(w, http.StatusNotFound, "IP restriction configuration not found on service", nil)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
}

func (h *ManagementHandler) AddServiceIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.IPRestrictionConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.AddPluginToService(serviceId, ipRestrictionToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add IP restriction configuration to service", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Service IP restriction configuration added successfully", req)
}

func (h *ManagementHandler) UpdateServiceIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.IPRestrictionConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.UpdatePluginInService(serviceId, "ip-restriction", ipRestrictionToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update service IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Service IP restriction configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteServiceIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	if err := h.Manager.DeletePluginFromService(serviceId, "ip-restriction"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete service IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Service IP restriction configuration deleted successfully", nil)
}

func (h *ManagementHandler) GetRouteIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	cfg := h.Manager.GetConfig()
	for _, rt := range cfg.Routes {
		if rt.Id == routeId {
			for _, p := range rt.Plugins {
				if p.Name == "ip-restriction" {
					helper.RespondSuccess(w, http.StatusOK, "Route IP restriction configuration fetched successfully", parseIPRestrictionConfig(p))
					return
				}
			}
			helper.RespondError(w, http.StatusNotFound, "IP restriction configuration not found on route", nil)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Route not found", nil)
}

func (h *ManagementHandler) AddRouteIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.IPRestrictionConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.AddPluginToRoute(routeId, ipRestrictionToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add IP restriction configuration to route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Route IP restriction configuration added successfully", req)
}

func (h *ManagementHandler) UpdateRouteIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.IPRestrictionConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.UpdatePluginInRoute(routeId, "ip-restriction", ipRestrictionToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update route IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Route IP restriction configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteRouteIPRestrictionConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	if err := h.Manager.DeletePluginFromRoute(routeId, "ip-restriction"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete route IP restriction configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Route IP restriction configuration deleted successfully", nil)
}

// === REQUEST TRANSFORM CONFIG HANDLERS ===

func parseRequestTransformConfig(p config.PluginConfig) domain.RequestTransformConfig {
	return domain.RequestTransformConfig{
		Enabled:               p.Enabled,
		AddHeaders:            extractStringMap(p.Config["add_headers"]),
		SetHeaders:            extractStringMap(p.Config["set_headers"]),
		RemoveHeaders:         extractStringSlice(p.Config["remove_headers"]),
		AddQueryParams:        extractStringMap(p.Config["add_query_params"]),
		RemoveQueryParams:     extractStringSlice(p.Config["remove_query_params"]),
		AddResponseHeaders:    extractStringMap(p.Config["add_response_headers"]),
		SetResponseHeaders:    extractStringMap(p.Config["set_response_headers"]),
		RemoveResponseHeaders: extractStringSlice(p.Config["remove_response_headers"]),
	}
}

func requestTransformToPlugin(dto domain.RequestTransformConfig) config.PluginConfig {
	return config.PluginConfig{
		Name:    "request-transform",
		Enabled: dto.Enabled,
		Config: map[string]interface{}{
			"add_headers":             dto.AddHeaders,
			"set_headers":             dto.SetHeaders,
			"remove_headers":          dto.RemoveHeaders,
			"add_query_params":        dto.AddQueryParams,
			"remove_query_params":     dto.RemoveQueryParams,
			"add_response_headers":    dto.AddResponseHeaders,
			"set_response_headers":    dto.SetResponseHeaders,
			"remove_response_headers": dto.RemoveResponseHeaders,
		},
	}
}

func (h *ManagementHandler) GetRequestTransformConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	for _, p := range cfg.GlobalPlugins {
		if p.Name == "request-transform" {
			helper.RespondSuccess(w, http.StatusOK, "Request transform configuration fetched successfully", parseRequestTransformConfig(p))
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Request transform configuration not found", nil)
}

func (h *ManagementHandler) AddRequestTransformConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.RequestTransformConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.AddGlobalPlugin(requestTransformToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Request transform configuration added successfully", req)
}

func (h *ManagementHandler) UpdateRequestTransformConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.RequestTransformConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.UpdateGlobalPlugin("request-transform", requestTransformToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Request transform configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteRequestTransformConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := h.Manager.DeleteGlobalPlugin("request-transform"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Request transform configuration deleted successfully", nil)
}

func (h *ManagementHandler) GetServiceRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()
	for _, s := range cfg.Services {
		if s.Id == serviceId {
			for _, p := range s.Plugins {
				if p.Name == "request-transform" {
					helper.RespondSuccess(w, http.StatusOK, "Service request transform configuration fetched successfully", parseRequestTransformConfig(p))
					return
				}
			}
			helper.RespondError(w, http.StatusNotFound, "Request transform configuration not found on service", nil)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
}

func (h *ManagementHandler) AddServiceRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.RequestTransformConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.AddPluginToService(serviceId, requestTransformToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add request transform configuration to service", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Service request transform configuration added successfully", req)
}

func (h *ManagementHandler) UpdateServiceRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	var req domain.RequestTransformConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.UpdatePluginInService(serviceId, "request-transform", requestTransformToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update service request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Service request transform configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteServiceRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	if err := h.Manager.DeletePluginFromService(serviceId, "request-transform"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete service request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Service request transform configuration deleted successfully", nil)
}

func (h *ManagementHandler) GetRouteRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	cfg := h.Manager.GetConfig()
	for _, rt := range cfg.Routes {
		if rt.Id == routeId {
			for _, p := range rt.Plugins {
				if p.Name == "request-transform" {
					helper.RespondSuccess(w, http.StatusOK, "Route request transform configuration fetched successfully", parseRequestTransformConfig(p))
					return
				}
			}
			helper.RespondError(w, http.StatusNotFound, "Request transform configuration not found on route", nil)
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Route not found", nil)
}

func (h *ManagementHandler) AddRouteRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.RequestTransformConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.AddPluginToRoute(routeId, requestTransformToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusConflict, "Failed to add request transform configuration to route", err)
		return
	}
	helper.RespondSuccess(w, http.StatusCreated, "Route request transform configuration added successfully", req)
}

func (h *ManagementHandler) UpdateRouteRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	var req domain.RequestTransformConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if err := h.Manager.UpdatePluginInRoute(routeId, "request-transform", requestTransformToPlugin(req)); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to update route request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusOK, "Route request transform configuration updated successfully", req)
}

func (h *ManagementHandler) DeleteRouteRequestTransformConfig(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	routeId := ps.ByName("routeId")
	if err := h.Manager.DeletePluginFromRoute(routeId, "request-transform"); err != nil {
		helper.RespondError(w, http.StatusNotFound, "Failed to delete route request transform configuration", err)
		return
	}
	helper.RespondSuccess(w, http.StatusNoContent, "Route request transform configuration deleted successfully", nil)
}

// === GATEWAY STATUS, CIRCUIT BREAKER & CONFIG SNAPSHOT HANDLERS ===

func (h *ManagementHandler) buildServiceBreakerStatus(svc config.Service) domain.ServiceCircuitBreakerStatus {
	status := domain.ServiceCircuitBreakerStatus{
		ServiceId:   svc.Id,
		ServiceName: svc.Name,
		State:       "CLOSED",
	}
	if h.GetCircuitBreakerStats != nil {
		state, failures, lastFail := h.GetCircuitBreakerStats(svc.Id)
		status.State = state
		status.FailureCount = failures
		if !lastFail.IsZero() {
			status.LastFailureTime = lastFail.UTC().Format(time.RFC3339)
		}
	}
	return status
}

func (h *ManagementHandler) GetGatewayStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	var uptime int64
	if !h.StartTime.IsZero() {
		uptime = int64(time.Since(h.StartTime).Seconds())
	}

	breakers := make([]domain.ServiceCircuitBreakerStatus, len(cfg.Services))
	for i, svc := range cfg.Services {
		breakers[i] = h.buildServiceBreakerStatus(svc)
	}

	resp := domain.GatewayStatusResponse{
		Status:             "healthy",
		UptimeSeconds:      uptime,
		UpstreamsCount:     len(cfg.Upstreams),
		ServicesCount:      len(cfg.Services),
		RoutesCount:        len(cfg.Routes),
		GlobalPluginsCount: len(cfg.GlobalPlugins),
		CircuitBreakers:    breakers,
	}
	helper.RespondSuccess(w, http.StatusOK, "Gateway status fetched successfully", resp)
}

func (h *ManagementHandler) GetServiceCircuitBreaker(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()
	for _, svc := range cfg.Services {
		if svc.Id == serviceId {
			helper.RespondSuccess(w, http.StatusOK, "Service circuit breaker status fetched successfully", h.buildServiceBreakerStatus(svc))
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
}

func (h *ManagementHandler) ResetServiceCircuitBreaker(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	serviceId := ps.ByName("serviceId")
	cfg := h.Manager.GetConfig()
	for _, svc := range cfg.Services {
		if svc.Id == serviceId {
			if h.ResetCircuitBreaker != nil {
				h.ResetCircuitBreaker(serviceId)
			}
			helper.RespondSuccess(w, http.StatusOK, "Service circuit breaker reset to CLOSED", h.buildServiceBreakerStatus(svc))
			return
		}
	}
	helper.RespondError(w, http.StatusNotFound, "Service not found", nil)
}

func (h *ManagementHandler) GetFullConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	helper.RespondSuccess(w, http.StatusOK, "Full configuration fetched successfully", cfg)
}

func (h *ManagementHandler) ResetGatewayMetrics(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if h.ResetMetrics != nil {
		h.ResetMetrics()
	}
	helper.RespondSuccess(w, http.StatusOK, "Gateway metrics reset successfully", nil)
}
