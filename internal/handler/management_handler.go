package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/domain"
	"github.com/haiser1/go-api-gateway/internal/helper"
	"github.com/julienschmidt/httprouter"
)

// ManagementHandler menampung logika untuk API manajemen
type ManagementHandler struct {
	Manager *config.Manager
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
	if req.Name == "" || len(req.Paths) == 0 || req.ServiceId == "" {
		helper.RespondError(w, http.StatusBadRequest, "Missing required fields: name, paths, serviceId", nil)
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
	if req.Name == "" || len(req.Paths) == 0 || req.ServiceId == "" {
		helper.RespondError(w, http.StatusBadRequest, "Missing required fields: name, paths, serviceId", nil)
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
