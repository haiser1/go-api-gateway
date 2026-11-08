package domain

// All request DTOs carry `validate:"..."` tags checked by the centralized
// validator package (validator.ValidateStruct) at the Management API
// boundary. The binding tags are legacy and unused.

// Plugin adalah DTO untuk plugin (tanpa ID, dikelola by Name)
type Plugin struct {
	Name    string                 `json:"name" validate:"required"`
	Enabled bool                   `json:"enabled,omitempty"`
	Config  map[string]interface{} `json:"config"`
}

// === UPSTREAM DTOs ===

type HealthCheckDTO struct {
	Path     string `json:"path"`
	Interval string `json:"interval"`
}

type UpstreamTargetDTO struct {
	Host        string          `json:"host" binding:"required" validate:"required"`
	Port        int             `json:"port" validate:"omitempty,min=1,max=65535"`
	Weight      int             `json:"weight" validate:"omitempty,min=1"`
	HealthCheck *HealthCheckDTO `json:"health_check,omitempty"`
}

type AddUpstreamRequest struct {
	Name      string              `json:"name" binding:"required" validate:"required"`
	Algorithm string              `json:"algorithm" validate:"omitempty,oneof=round-robin weighted-round-robin"`
	Targets   []UpstreamTargetDTO `json:"targets" binding:"required" validate:"required,min=1,dive"`
}

type UpdateUpstreamRequest struct {
	Name      string              `json:"name" binding:"required" validate:"required"`
	Algorithm string              `json:"algorithm" validate:"omitempty,oneof=round-robin weighted-round-robin"`
	Targets   []UpstreamTargetDTO `json:"targets" binding:"required" validate:"required,min=1,dive"`
}

type UpstreamDetailResponse struct {
	Id        string              `json:"id"`
	Name      string              `json:"name"`
	Algorithm string              `json:"algorithm"`
	Targets   []UpstreamTargetDTO `json:"targets"`
}

// === SERVICE DTOs ===

type AddServiceRequest struct {
	Name       string   `json:"name" binding:"required" validate:"required"`
	UpstreamId string   `json:"upstream_id" binding:"required" validate:"required"`
	Protocol   string   `json:"protocol" validate:"omitempty,oneof=http https"`
	Plugins    []Plugin `json:"plugins,omitempty" validate:"omitempty,dive"`

	// Timeout settings (in seconds)
	Timeout        int `json:"timeout,omitempty" validate:"omitempty,min=1"`
	ConnectTimeout int `json:"connect_timeout,omitempty" validate:"omitempty,min=1"`
	ReadTimeout    int `json:"read_timeout,omitempty" validate:"omitempty,min=1"`

	// Retry settings
	Retries      int     `json:"retries,omitempty" validate:"omitempty,min=0"`
	RetryBackoff float64 `json:"retry_backoff,omitempty" validate:"omitempty,min=0"`
}

type UpdateServiceRequest struct {
	Name       string   `json:"name" binding:"required" validate:"required"`
	UpstreamId string   `json:"upstream_id" binding:"required" validate:"required"`
	Protocol   string   `json:"protocol" validate:"omitempty,oneof=http https"`
	Plugins    []Plugin `json:"plugins,omitempty" validate:"omitempty,dive"`

	// Timeout settings (in seconds)
	Timeout        int `json:"timeout,omitempty" validate:"omitempty,min=1"`
	ConnectTimeout int `json:"connect_timeout,omitempty" validate:"omitempty,min=1"`
	ReadTimeout    int `json:"read_timeout,omitempty" validate:"omitempty,min=1"`

	// Retry settings
	Retries      int     `json:"retries,omitempty" validate:"omitempty,min=0"`
	RetryBackoff float64 `json:"retry_backoff,omitempty" validate:"omitempty,min=0"`
}

// === ROUTE DTOs ===

type AddRouteRequest struct {
	Name        string   `json:"name" binding:"required" validate:"required"`
	Methods     []string `json:"methods" binding:"required" validate:"required,min=1,unique,dive,oneof=GET POST PUT PATCH DELETE HEAD OPTIONS TRACE CONNECT"`
	Paths       []string `json:"paths" binding:"required" validate:"required,min=1,unique,dive,startswith=/"`
	ServiceId   string   `json:"service_id" binding:"required" validate:"required"`
	StripPrefix bool     `json:"strip_prefix,omitempty"`
	Plugins     []Plugin `json:"plugins,omitempty" validate:"omitempty,dive"`
}

type UpdateRouteRequest struct {
	Name        string   `json:"name" binding:"required" validate:"required"`
	Methods     []string `json:"methods" binding:"required" validate:"required,min=1,unique,dive,oneof=GET POST PUT PATCH DELETE HEAD OPTIONS TRACE CONNECT"`
	Paths       []string `json:"paths" binding:"required" validate:"required,min=1,unique,dive,startswith=/"`
	ServiceId   string   `json:"service_id" binding:"required" validate:"required"`
	StripPrefix bool     `json:"strip_prefix,omitempty"`
	Plugins     []Plugin `json:"plugins,omitempty" validate:"omitempty,dive"`
}

// === RESPONSE DTOs ===

// RouteDetail adalah DTO untuk menampilkan rute di dalam ServiceDetail
type RouteDetail struct {
	Id      string   `json:"id"`
	Name    string   `json:"name"`
	Methods []string `json:"methods"`
	Paths   []string `json:"paths"`
	Plugins []Plugin `json:"plugins,omitempty"`
}

// ServiceDetailResponse adalah DTO kustom untuk endpoint GetServiceById
type ServiceDetailResponse struct {
	Id         string        `json:"id"`
	Name       string        `json:"name"`
	UpstreamId string        `json:"upstream_id"`
	Protocol   string        `json:"protocol"`
	Plugins    []Plugin      `json:"plugins,omitempty"`
	Routes     []RouteDetail `json:"routes"`

	// Timeout settings
	Timeout        int `json:"timeout,omitempty"`
	ConnectTimeout int `json:"connect_timeout,omitempty"`
	ReadTimeout    int `json:"read_timeout,omitempty"`

	// Retry settings
	Retries      int     `json:"retries,omitempty"`
	RetryBackoff float64 `json:"retry_backoff,omitempty"`
}

// ServiceSnapshot adalah DTO simpel untuk disematkan dalam RouteDetail
type ServiceSnapshot struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// RouteDetailResponse adalah DTO kustom untuk endpoint GetRouteById
type RouteDetailResponse struct {
	Id      string           `json:"id"`
	Name    string           `json:"name"`
	Methods []string         `json:"methods"`
	Paths   []string         `json:"paths"`
	Plugins []Plugin         `json:"plugins,omitempty"`
	Service *ServiceSnapshot `json:"service"`
}

// CORSConfig adalah DTO untuk konfigurasi CORS spesifik
type CORSConfig struct {
	Enabled          bool     `json:"enabled"`
	AllowedOrigins   []string `json:"allowed_origins"`
	AllowedMethods   []string `json:"allowed_methods"`
	AllowedHeaders   []string `json:"allowed_headers"`
	ExposedHeaders   []string `json:"exposed_headers,omitempty"`
	AllowCredentials bool     `json:"allow_credentials"`
	MaxAge           int      `json:"max_age" validate:"omitempty,min=0"`
}

// IPRestrictionConfig adalah DTO untuk konfigurasi plugin ip-restriction / bot-detection
type IPRestrictionConfig struct {
	Enabled           bool     `json:"enabled"`
	AllowedIPs        []string `json:"allowed_ips,omitempty"`
	DeniedIPs         []string `json:"denied_ips,omitempty"`
	BlockedBots       []string `json:"blocked_bots,omitempty"`
	TrustForwardedFor bool     `json:"trust_forwarded_for"`
	StatusCode        int      `json:"status_code,omitempty" validate:"omitempty,min=400,max=599"`
	Message           string   `json:"message,omitempty"`
}

// RequestTransformConfig adalah DTO untuk konfigurasi plugin request-transform
type RequestTransformConfig struct {
	Enabled               bool              `json:"enabled"`
	AddHeaders            map[string]string `json:"add_headers,omitempty"`
	SetHeaders            map[string]string `json:"set_headers,omitempty"`
	RemoveHeaders         []string          `json:"remove_headers,omitempty"`
	AddQueryParams        map[string]string `json:"add_query_params,omitempty"`
	RemoveQueryParams     []string          `json:"remove_query_params,omitempty"`
	AddResponseHeaders    map[string]string `json:"add_response_headers,omitempty"`
	SetResponseHeaders    map[string]string `json:"set_response_headers,omitempty"`
	RemoveResponseHeaders []string          `json:"remove_response_headers,omitempty"`
}

// ServiceCircuitBreakerStatus merepresentasikan status circuit breaker per service
type ServiceCircuitBreakerStatus struct {
	ServiceId       string `json:"service_id"`
	ServiceName     string `json:"service_name"`
	State           string `json:"state"`
	FailureCount    int    `json:"failure_count"`
	LastFailureTime string `json:"last_failure_time,omitempty"`
}

// GatewayStatusResponse merepresentasikan kesehatan dan ringkasan runtime gateway
type GatewayStatusResponse struct {
	Status             string                        `json:"status"`
	UptimeSeconds      int64                         `json:"uptime_seconds"`
	UpstreamsCount     int                           `json:"upstreams_count"`
	ServicesCount      int                           `json:"services_count"`
	RoutesCount        int                           `json:"routes_count"`
	GlobalPluginsCount int                           `json:"global_plugins_count"`
	CircuitBreakers    []ServiceCircuitBreakerStatus `json:"circuit_breakers"`
}
