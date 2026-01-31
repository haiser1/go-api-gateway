package gateway

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/rs/zerolog/log"
)

type transportEntry struct {
	transport      *http.Transport
	connectTimeout int
	readTimeout    int
}

// serviceTransports caches HTTP transports by service ID
var serviceTransports sync.Map

// GetServiceTransport returns a cached transport or creates a new one
func GetServiceTransport(service config.Service) *http.Transport {
	// Check cache
	if cached, ok := serviceTransports.Load(service.Id); ok {
		entry := cached.(*transportEntry)
		// Reuse if timeouts match
		if entry.connectTimeout == service.ConnectTimeout && entry.readTimeout == service.ReadTimeout {
			return entry.transport
		}
	}

	// Create new transport with service-specific timeouts
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(service.ConnectTimeout) * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: time.Duration(service.ReadTimeout) * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
	}

	// Cache it
	serviceTransports.Store(service.Id, &transportEntry{
		transport:      transport,
		connectTimeout: service.ConnectTimeout,
		readTimeout:    service.ReadTimeout,
	})

	log.Info().Str("service", service.Id).Msg("Created new transport for service")
	return transport
}

// Circuit Breaker Transport Wrapper
type circuitBreakerTransport struct {
	wrapped   http.RoundTripper
	serviceId string
}

// NewCircuitBreakerTransport creates a transport wrapper with circuit breaker
func NewCircuitBreakerTransport(transport http.RoundTripper, serviceId string) *circuitBreakerTransport {
	return &circuitBreakerTransport{
		wrapped:   transport,
		serviceId: serviceId,
	}
}

func (t *circuitBreakerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cb := GetCircuitBreaker(t.serviceId)

	if !cb.AllowRequest() {
		return nil, fmt.Errorf("circuit breaker is open for service %s", t.serviceId)
	}

	resp, err := t.wrapped.RoundTrip(r)

	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		cb.RecordFailure()
		if err != nil {
			log.Error().Err(err).Str("service", t.serviceId).Msg("Upstream failure")
		} else {
			log.Warn().Int("status", resp.StatusCode).Str("service", t.serviceId).Msg("Upstream response error")
		}
	} else {
		cb.RecordSuccess()
	}

	return resp, err
}
