package middleware

import (
	"net/http"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

type keyValue struct {
	key   string
	value string
}

// transformResponseWriter wraps http.ResponseWriter to apply response header transformations
// right before headers are flushed to the client.
type transformResponseWriter struct {
	http.ResponseWriter
	addHeaders    []keyValue
	setHeaders    []keyValue
	removeHeaders []string
	wroteHeader   bool
}

func (rw *transformResponseWriter) applyHeaders() {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	h := rw.ResponseWriter.Header()
	for _, kv := range rw.addHeaders {
		h.Add(kv.key, kv.value)
	}
	for _, kv := range rw.setHeaders {
		h.Set(kv.key, kv.value)
	}
	for _, k := range rw.removeHeaders {
		h.Del(k)
	}
}

func (rw *transformResponseWriter) WriteHeader(code int) {
	rw.applyHeaders()
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *transformResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func parseKVSlice(raw interface{}) []keyValue {
	var result []keyValue
	switch m := raw.(type) {
	case map[string]interface{}:
		result = make([]keyValue, 0, len(m))
		for k, v := range m {
			if s, ok := v.(string); ok && k != "" {
				result = append(result, keyValue{key: k, value: s})
			}
		}
	case map[string]string:
		result = make([]keyValue, 0, len(m))
		for k, v := range m {
			if k != "" {
				result = append(result, keyValue{key: k, value: v})
			}
		}
	}
	return result
}

func parseStringSlice(raw interface{}) []string {
	var result []string
	switch s := raw.(type) {
	case []interface{}:
		result = make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok && str != "" {
				result = append(result, str)
			}
		}
	case []string:
		result = make([]string, 0, len(s))
		for _, str := range s {
			if str != "" {
				result = append(result, str)
			}
		}
	}
	return result
}

// NewRequestTransformPlugin creates a pre-compiled request/response transformation middleware.
//
// Supported config keys:
//   - add_headers (map[string]string): Append headers to upstream request
//   - set_headers (map[string]string): Set/overwrite headers on upstream request
//   - remove_headers ([]string): Remove headers from upstream request
//   - add_query_params (map[string]string): Set/append query parameters on upstream request URL
//   - remove_query_params ([]string): Remove query parameters from upstream request URL
//   - add_response_headers (map[string]string): Append headers to downstream response
//   - set_response_headers (map[string]string): Set/overwrite headers on downstream response
//   - remove_response_headers ([]string): Remove headers from downstream response
func NewRequestTransformPlugin(config map[string]interface{}) gateway.PluginMiddleware {
	addReqHeaders := parseKVSlice(config["add_headers"])
	setReqHeaders := parseKVSlice(config["set_headers"])
	removeReqHeaders := parseStringSlice(config["remove_headers"])

	addQueryParams := parseKVSlice(config["add_query_params"])
	removeQueryParams := parseStringSlice(config["remove_query_params"])

	addRespHeaders := parseKVSlice(config["add_response_headers"])
	setRespHeaders := parseKVSlice(config["set_response_headers"])
	removeRespHeaders := parseStringSlice(config["remove_response_headers"])

	hasQueryMutations := len(addQueryParams) > 0 || len(removeQueryParams) > 0
	hasRespMutations := len(addRespHeaders) > 0 || len(setRespHeaders) > 0 || len(removeRespHeaders) > 0

	return gateway.PluginFunc{
		NameStr: "request-transform",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			// 1. Mutate Request Headers
			for _, kv := range addReqHeaders {
				r.Header.Add(kv.key, kv.value)
			}
			for _, kv := range setReqHeaders {
				r.Header.Set(kv.key, kv.value)
			}
			for _, k := range removeReqHeaders {
				r.Header.Del(k)
			}

			// 2. Mutate Query Parameters (only parse URL query when configured)
			if hasQueryMutations {
				q := r.URL.Query()
				for _, kv := range addQueryParams {
					q.Set(kv.key, kv.value)
				}
				for _, k := range removeQueryParams {
					q.Del(k)
				}
				r.URL.RawQuery = q.Encode()
			}

			// 3. Mutate Response Headers (only wrap ResponseWriter when configured)
			if hasRespMutations {
				trw := &transformResponseWriter{
					ResponseWriter: w,
					addHeaders:     addRespHeaders,
					setHeaders:     setRespHeaders,
					removeHeaders:  removeRespHeaders,
				}
				next.ServeHTTP(trw, r)
				// Ensure headers are applied even if the upstream handler never called Write/WriteHeader
				trw.applyHeaders()
				return true
			}

			next.ServeHTTP(w, r)
			return true
		},
	}
}

func init() {
	gateway.RegisterPlugin("request-transform", NewRequestTransformPlugin)
	gateway.RegisterPlugin("request-transformation", NewRequestTransformPlugin)
}
