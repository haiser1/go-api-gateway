package gateway

import (
	"net/http"
)

// PluginMiddleware adalah interface untuk semua plugin
type PluginMiddleware interface {
	Name() string
	Execute(http.ResponseWriter, *http.Request, http.Handler) bool
}

// PluginFunc memudahkan plugin sederhana berbasis fungsi
type PluginFunc struct {
	NameStr string
	Handler func(http.ResponseWriter, *http.Request, http.Handler) bool
}

func (p PluginFunc) Name() string {
	return p.NameStr
}

func (p PluginFunc) Execute(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	return p.Handler(w, r, next)
}

// PluginBuilder adalah fungsi yang membuat PluginMiddleware dari konfigurasi
type PluginBuilder func(config map[string]interface{}) PluginMiddleware

// pluginRegistry menyimpan semua plugin yang terdaftar
var pluginRegistry = make(map[string]PluginBuilder)

// RegisterPlugin mendaftarkan plugin builder dengan nama tertentu
func RegisterPlugin(name string, builder PluginBuilder) {
	pluginRegistry[name] = builder
}
