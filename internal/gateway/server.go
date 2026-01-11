package gateway

import (
	"context"
	"log"
	"net/http"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/julienschmidt/httprouter"
)

type Server struct {
	httpServer *http.Server
	cfgManager *config.Manager
	router     *httprouter.Router
	proxy      *Proxy // DIPERBARUI: Menampung proxy
}

func NewServer(cfgManager *config.Manager) *Server {
	router := httprouter.New()

	// 1. Buat proxy publik (Data Plane)
	// Kita inisialisasi dengan config awal
	proxy := NewProxy(cfgManager.GetConfig())

	// 2. Daftarkan rute manajemen (Control Plane)
	RegisterManagementRoutes(router, cfgManager)

	// 3. Daftarkan rute proxy publik (Data Plane)
	// Ini akan menangani SEMUA rute publik
	// httprouter tidak bisa menangani 'catch-all' dengan baik,
	// jadi kita gunakan http.Handler di rute '/'.
	// Jika rute API Anda HANYA di '/api/...', ini aman.
	// Jika Anda ingin proxy di root, kita perlu router yang berbeda.
	// Untuk saat ini, kita akan mendaftarkan NotAllowed dan NotFound handler
	// untuk menangkap semua trafik yang tidak cocok dan meneruskannya ke proxy.
	// Ini adalah cara httprouter menangani 'catch-all'.
	router.NotFound = http.HandlerFunc(proxy.ServeHTTP)
	router.MethodNotAllowed = http.HandlerFunc(proxy.ServeHTTP)

	s := &Server{
		cfgManager: cfgManager,
		router:     router,
		proxy:      proxy, // Simpan proxy
		httpServer: &http.Server{
			Addr:    ":8080", // Nanti pindahkan ke config
			Handler: router,
		},
	}
	return s
}

func (s *Server) Start() error {
	go s.watchConfigChanges()
	log.Println("Server started on port 8080")
	return s.httpServer.ListenAndServe()
}

func (s *Server) watchConfigChanges() {
	for range s.cfgManager.Reload {
		log.Println("Config file changed, reloading proxy routes...")
		s.reloadRoutes()
	}
}

// reloadRoutes SEKARANG MEMILIKI LOGIKA
func (s *Server) reloadRoutes() {
	// Dapatkan config baru
	cfg := s.cfgManager.GetConfig()

	// Perintahkan proxy untuk memperbarui tabel routingnya (secara thread-safe)
	s.proxy.UpdateRoutes(cfg)

	log.Printf("Proxy reloaded. Services: %d, Routes: %d, Global Plugins: %d",
		len(cfg.Services), len(cfg.Routes), len(cfg.GlobalPlugins))
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down API Gateway with graceful shutdown...")
	return s.httpServer.Shutdown(ctx)
}
