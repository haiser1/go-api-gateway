package domain

// Plugin adalah DTO untuk plugin (tanpa ID, dikelola by Name)
type Plugin struct {
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
}

// === DTO REQUEST (untuk POST/PUT) ===

type AddServiceRequest struct {
	Name     string   `json:"name" binding:"required"`
	Protocol string   `json:"protocol"`
	Host     string   `json:"host" binding:"required"`
	Port     int      `json:"port"`
	Plugins  []Plugin `json:"plugins,omitempty"`

	// Timeout settings (in seconds)
	Timeout        int `json:"timeout,omitempty"`
	ConnectTimeout int `json:"connect_timeout,omitempty"`
	ReadTimeout    int `json:"read_timeout,omitempty"`

	// Retry settings
	Retries      int     `json:"retries,omitempty"`
	RetryBackoff float64 `json:"retry_backoff,omitempty"`
}

type AddRouteRequest struct {
	Name      string   `json:"name" binding:"required"`
	Methods   []string `json:"methods" binding:"required"`
	Paths     []string `json:"paths" binding:"required"`
	ServiceId string   `json:"service_id" binding:"required"`
	Plugins   []Plugin `json:"plugins,omitempty"`
}

type UpdateServiceRequest struct {
	Name     string   `json:"name" binding:"required"`
	Protocol string   `json:"protocol"`
	Host     string   `json:"host" binding:"required"`
	Port     int      `json:"port"`
	Plugins  []Plugin `json:"plugins,omitempty"`

	// Timeout settings (in seconds)
	Timeout        int `json:"timeout,omitempty"`
	ConnectTimeout int `json:"connect_timeout,omitempty"`
	ReadTimeout    int `json:"read_timeout,omitempty"`

	// Retry settings
	Retries      int     `json:"retries,omitempty"`
	RetryBackoff float64 `json:"retry_backoff,omitempty"`
}

type UpdateRouteRequest struct {
	Name      string   `json:"name" binding:"required"`
	Methods   []string `json:"methods" binding:"required"`
	Paths     []string `json:"paths" binding:"required"`
	ServiceId string   `json:"service_id" binding:"required"`
	Plugins   []Plugin `json:"plugins,omitempty"`
}

// === DTO RESPONSE (untuk GET detail) ===

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
	Id       string        `json:"id"`
	Name     string        `json:"name"`
	Protocol string        `json:"protocol"`
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Plugins  []Plugin      `json:"plugins,omitempty"`
	Routes   []RouteDetail `json:"routes"`

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
