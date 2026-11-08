package validator

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// supportedAlgorithms/supportedProtocols were consolidated into `oneof`
// validate tags on config.Upstream.Algorithm and config.Service.Protocol
// (single source of rules).

// supportedMethods is DOCUMENTATION ONLY: the HTTP method set a route may
// declare is enforced by the oneof tag on Route.Methods.
var supportedMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"PATCH":   true,
	"DELETE":  true,
	"HEAD":    true,
	"OPTIONS": true,
	"TRACE":   true,
}

// knownPlugins holds plugin names registered by the runtime (wired from
// gateway.RegisterPlugin). When the set is EMPTY (pure config builds, unit
// tests), plugin-name validation is skipped; otherwise unknown plugin names
// are rejected — a plugin the runtime does not ship silently degrades routes.
var knownPlugins = make(map[string]bool)

// RegisterKnownPlugin declares a plugin name as valid configuration. Called
// by the runtime when it registers a plugin implementation.
func RegisterKnownPlugin(name string) {
	if name != "" {
		knownPlugins[name] = true
	}
}

// validateSemantics performs every rule that needs more than one field or
// awareness of the whole configuration graph: identity duplication, entity
// references, targets, plugins and load balancing. Errors are aggregated —
// the caller fixes as many problems as possible in one attempt.
//
// NOTE: single-field shape rules (required fields, numeric ranges, enums)
// live in the `validate:"..."` tags on internal/config structs (see
// structural.go). Rules appear EXACTLY ONCE: neither layer duplicates the
// other.
func validateSemantics(cfg *config.Config) ValidationErrors {
	var errs ValidationErrors

	if cfg.Auth != nil {
		if err := cfg.Auth.Validate(); err != nil {
			errs = append(errs, ValidationError{
				Field:   "Auth",
				Code:    CodeInvalidValue,
				Message: err.Error(),
			})
		}
	}
	if _, err := config.ResolveAuthConfig(cfg); err != nil {
		errs = append(errs, ValidationError{
			Field:   "Auth",
			Code:    CodeInvalidValue,
			Message: err.Error(),
		})
	}

	upstreamIds := make(map[string]int, len(cfg.Upstreams))
	upstreamNames := make(map[string]int, len(cfg.Upstreams))
	for i := range cfg.Upstreams {
		u := &cfg.Upstreams[i]
		errs = append(errs,
			checkIdentity("Upstreams", i, "id", u.Id, upstreamIds)...)
		errs = append(errs,
			checkIdentity("Upstreams", i, "name", u.Name, upstreamNames)...)
		if len(u.Targets) == 0 {
			errs = append(errs, ValidationError{
				Field:   fieldAt("Upstreams", i, "Targets"),
				Code:    CodeInvalidValue,
				Message: fmt.Sprintf("upstream %q has no targets; routes resolved through it cannot serve traffic", u.Id),
			})
		}
		for t := range u.Targets {
			errs = append(errs, validateTarget(u.Id, i, t, &u.Targets[t])...)
		}
	}

	serviceIds := make(map[string]int, len(cfg.Services))
	serviceNames := make(map[string]int, len(cfg.Services))
	for i := range cfg.Services {
		s := &cfg.Services[i]
		errs = append(errs, checkIdentity("Services", i, "id", s.Id, serviceIds)...)
		errs = append(errs, checkIdentity("Services", i, "name", s.Name, serviceNames)...)
		if s.UpstreamId != "" {
			if _, ok := upstreamIds[s.UpstreamId]; !ok {
				errs = append(errs, ValidationError{
					Field:   fieldAt("Services", i, "UpstreamId"),
					Code:    CodeUnknownReference,
					Message: fmt.Sprintf("upstream %q referenced by service %q does not exist", s.UpstreamId, s.Id),
				})
			}
		}
		errs = append(errs, validatePlugins(s.Plugins, fmt.Sprintf("Services[%d].Plugins", i))...)
	}

	routeIds := make(map[string]int, len(cfg.Routes))
	routeNames := make(map[string]int, len(cfg.Routes))
	for i := range cfg.Routes {
		r := &cfg.Routes[i]
		errs = append(errs, checkIdentity("Routes", i, "id", r.Id, routeIds)...)
		errs = append(errs, checkIdentity("Routes", i, "name", r.Name, routeNames)...)
		if r.ServiceId != "" {
			if _, ok := serviceIds[r.ServiceId]; !ok {
				errs = append(errs, ValidationError{
					Field:   fieldAt("Routes", i, "ServiceId"),
					Code:    CodeUnknownReference,
					Message: fmt.Sprintf("service %q referenced by route %q does not exist", r.ServiceId, r.Id),
				})
			}
		}
		for p := range r.Paths {
			if err := validatePathPattern(r.Paths[p]); err != nil {
				errs = append(errs, ValidationError{
					Field:   fieldAt("Routes", i, fmt.Sprintf("Paths[%d]", p)),
					Code:    CodeInvalidFormat,
					Message: err.Error(),
				})
			}
		}
		errs = append(errs, validatePlugins(r.Plugins, fmt.Sprintf("Routes[%d].Plugins", i))...)
	}

	globalPlugins := make(map[string]int, len(cfg.GlobalPlugins))
	for i := range cfg.GlobalPlugins {
		errs = append(errs, checkIdentity("GlobalPlugins", i, "name", cfg.GlobalPlugins[i].Name, globalPlugins)...)
	}
	errs = append(errs, validatePlugins(cfg.GlobalPlugins, "GlobalPlugins")...)

	errs = append(errs, validateRouteConflicts(cfg)...)

	return errs
}

// checkIdentity reports a duplicate value of an identity field.
func checkIdentity(scope string, idx int, name string, value string, seen map[string]int) ValidationErrors {
	if value == "" {
		return nil
	}
	if first, ok := seen[value]; ok {
		return ValidationErrors{{
			Field: fieldAt(scope, idx, name),
			Code:  CodeDuplicate,
			Message: fmt.Sprintf("duplicate %s %q already declared at %s[%d]",
				name, value, scope, first),
		}}
	}
	seen[value] = idx
	return nil
}

// validateTarget performs value checks on a target that `validate:"…"`
// tags cannot express: the host must be free of control characters and
// parse as a host. Numeric shapes (port/weight ranges) are enforced by the
// tag layer and NOT repeated here.
func validateTarget(upstreamId string, uIdx, tIdx int, t *config.UpstreamTarget) ValidationErrors {
	path := fmt.Sprintf("Upstreams[%d].Targets[%d]", uIdx, tIdx)
	var errs ValidationErrors
	if strings.TrimSpace(t.Host) == "" || strings.ContainsAny(t.Host, " \t\x00\n\r\x7f") {
		errs = append(errs, ValidationError{
			Field:   path + ".Host",
			Code:    CodeInvalidValue,
			Message: fmt.Sprintf("upstream %q target %d: host %q is not a usable host name or IP", upstreamId, tIdx, t.Host),
		})
	}
	if _, err := url.Parse("http://" + t.Host); err != nil {
		errs = append(errs, ValidationError{
			Field:   path + ".Host",
			Code:    CodeInvalidValue,
			Message: fmt.Sprintf("upstream %q target %d: host %q is not a valid host: %v", upstreamId, tIdx, t.Host, err),
		})
	}
	return errs
}

// validatePathPattern enforces the route path syntax accepted by the radix
// tree: absolute, no whitespace, no control characters.
func validatePathPattern(p string) error {
	if p == "" || !strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q must be absolute (start with '/')", p)
	}
	if strings.ContainsAny(p, " \t\x00\n\r\x7f") {
		return fmt.Errorf("path %q contains empty/control characters and is not a valid pattern", p)
	}
	return nil
}

// validatePlugins validates every plugin in the given scope: names must be
// registered by the runtime (when any are registered at all) and configs
// must match the plugin's declared contract.
func validatePlugins(plugins []config.PluginConfig, scope string) ValidationErrors {
	var errs ValidationErrors
	names := make(map[string]int, len(plugins))
	for i := range plugins {
		p := &plugins[i]
		if p.Name == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].Name", scope, i),
				Code:    CodeRequired,
				Message: "plugin name is required",
			})
			continue
		}
		if first, ok := names[p.Name]; ok {
			errs = append(errs, ValidationError{
				Field: fmt.Sprintf("%s[%d].Name", scope, i),
				Code:  CodeDuplicate,
				Message: fmt.Sprintf("duplicate plugin %q within the same scope (%s[%d])",
					p.Name, scope, first),
			})
		} else {
			names[p.Name] = i
		}
		if len(knownPlugins) > 0 && !knownPlugins[p.Name] {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("%s[%d].Name", scope, i),
				Code:    CodeUnknownPlugin,
				Message: fmt.Sprintf("plugin %q is not registered by the running gateway build", p.Name),
			})
		}
		if contract, ok := pluginContracts[p.Name]; ok {
			errs = append(errs, contract(scope, i, p.Config)...)
		}
	}
	return errs
}

// --- Plugin configuration contracts ---
//
// One contract function per plugin. The contract mirrors the keys the plugin
// builder actually reads at runtime; unknown keys stay legal (plugin configs
// are map[string]interface{} so forward-compatible), wrong TYPES or
// impossible VALUES are rejected instead of being silently ignored or
// defaulted away by the builder at request time.

type pluginContract func(scope string, idx int, cfgMap map[string]interface{}) ValidationErrors

var pluginContracts = map[string]pluginContract{
	"rate-limiting":          contractRateLimit,
	"auth":                   contractAuth,
	"authorization":          contractAuth,
	"cors":                   contractCORS,
	"ip-restriction":         contractIPRestriction,
	"bot-detection":          contractIPRestriction,
	"request-transform":      contractRequestTransform,
	"request-transformation": contractRequestTransform,
	"metrics":                contractMetrics,
	"prometheus":             contractMetrics,
	"logging":                noContract, // builder ignores config
	"logger":                 noContract,
}

func noContract(_ string, _ int, _ map[string]interface{}) ValidationErrors { return nil }

func contractRateLimit(scope string, idx int, m map[string]interface{}) ValidationErrors {
	if v, present := m["requests_per_minute"]; present {
		if rpm, ok := toNonNegativeNumber(v); !ok || rpm == 0 {
			return ValidationErrors{valueError(scope, idx, "requests_per_minute",
				fmt.Sprintf("requests_per_minute must be a number greater than 0, got %T(%v)", v, v))}
		}
	}
	return nil
}

func contractAuth(scope string, idx int, m map[string]interface{}) ValidationErrors {
	var errs ValidationErrors
	mode := strings.ToLower(toString(m["mode"]))
	if mode == "" {
		mode = strings.ToLower(toString(m["type"]))
	}
	switch mode {
	case "jwt", "api_key", "apikey", "api-key", "both", "":
	default:
		errs = append(errs, valueError(scope, idx, "mode",
			fmt.Sprintf("auth mode %q must be one of: api_key, jwt, both", toString(m["mode"]))))
	}

	if v, ok := m["alg"]; ok && toString(v) != "" {
		switch strings.ToUpper(toString(v)) {
		case "HS256", "RS256":
		default:
			errs = append(errs, valueError(scope, idx, "alg",
				fmt.Sprintf("JWT alg %q must be one of: HS256, RS256", toString(v))))
		}
	}

	// Credentials must never be supplied inline.
	if _, ok := m["jwt_key"]; ok {
		errs = append(errs, valueError(scope, idx, "jwt_key",
			"jwt_key is not allowed in config.yaml; use a secret reference"))
	}
	if v, ok := m["api_key"]; ok {
		if s, isString := v.(string); isString && strings.TrimSpace(s) != "" {
			errs = append(errs, valueError(scope, idx, "api_key",
				"plaintext api_key is not allowed in config.yaml; use api_key.secret_ref"))
		}
	}

	for _, key := range []string{
		"api_key_header", "jwt_authorization_header", "api_key_secret_ref",
		"jwt_secret_ref", "jwt_public_key_ref", "issuer", "audience", "kid", "leeway",
	} {
		if v, ok := m[key]; ok && toString(v) == "" {
			errs = append(errs, valueError(scope, idx, key, key+" must be a non-empty string"))
		}
	}
	if v, ok := m["leeway"]; ok && toString(v) != "" {
		if _, err := time.ParseDuration(toString(v)); err != nil {
			errs = append(errs, valueError(scope, idx, "leeway",
				fmt.Sprintf("leeway %q is not a valid duration", toString(v))))
		}
	}
	return errs
}

func contractCORS(scope string, idx int, m map[string]interface{}) ValidationErrors {
	var errs ValidationErrors
	listKeys := []string{"allowed_origins", "allowed_methods", "allowed_headers", "exposed_headers"}
	for _, key := range listKeys {
		if v, ok := m[key]; ok {
			if !isStringList(v) {
				errs = append(errs, valueError(scope, idx, key,
					fmt.Sprintf("%s must be a list of strings, got %T", key, v)))
			}
		}
	}
	if v, ok := m["allow_credentials"]; ok && !isBool(v) {
		errs = append(errs, valueError(scope, idx, "allow_credentials",
			fmt.Sprintf("allow_credentials must be a bool, got %T", v)))
	}
	if v, ok := m["max_age"]; ok {
		if _, ok := toNonNegativeNumber(v); !ok {
			errs = append(errs, valueError(scope, idx, "max_age",
				fmt.Sprintf("max_age must be a non-negative number, got %T(%v)", v, v)))
		}
	}
	return errs
}

func contractIPRestriction(scope string, idx int, m map[string]interface{}) ValidationErrors {
	var errs ValidationErrors
	for _, key := range []string{"allowed_ips", "whitelist", "denied_ips", "blacklist", "denied_user_agents", "blocked_bots"} {
		if v, ok := m[key]; ok && !isStringList(v) {
			errs = append(errs, valueError(scope, idx, key,
				fmt.Sprintf("%s must be a list of strings, got %T", key, v)))
		}
	}
	if v, ok := m["trust_forwarded_for"]; ok && !isBool(v) {
		errs = append(errs, valueError(scope, idx, "trust_forwarded_for",
			fmt.Sprintf("trust_forwarded_for must be a bool, got %T", v)))
	}
	if v, present := m["status_code"]; present {
		sc, ok := toNumber(v)
		if !ok || sc < 400 || sc >= 600 {
			errs = append(errs, valueError(scope, idx, "status_code",
				fmt.Sprintf("status_code must be a number in 400..599, got %T(%v)", v, v)))
		}
	}
	if v, ok := m["message"]; ok && toString(v) == "" {
		errs = append(errs, valueError(scope, idx, "message", "message must be a non-empty string"))
	}
	return errs
}

func contractRequestTransform(scope string, idx int, m map[string]interface{}) ValidationErrors {
	var errs ValidationErrors
	for _, key := range []string{"add_headers", "set_headers", "add_query_params", "add_response_headers", "set_response_headers"} {
		if v, ok := m[key]; ok && !isStringMap(v) {
			errs = append(errs, valueError(scope, idx, key,
				fmt.Sprintf("%s must be a string-to-string map, got %T", key, v)))
		}
	}
	for _, key := range []string{"remove_headers", "remove_query_params", "remove_response_headers"} {
		if v, ok := m[key]; ok && !isStringList(v) {
			errs = append(errs, valueError(scope, idx, key,
				fmt.Sprintf("%s must be a list of strings, got %T", key, v)))
		}
	}
	return errs
}

func contractMetrics(scope string, idx int, m map[string]interface{}) ValidationErrors {
	if v, ok := m["record_query"]; ok && !isBool(v) {
		return ValidationErrors{valueError(scope, idx, "record_query",
			fmt.Sprintf("record_query must be a bool, got %T", v))}
	}
	return nil
}

// --- small type helpers for plugin contract validation ---

func isStringList(v interface{}) bool {
	switch t := v.(type) {
	case []string:
		return true
	case []interface{}:
		for _, item := range t {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

func isStringMap(v interface{}) bool {
	switch t := v.(type) {
	case map[string]string:
		return true
	case map[string]interface{}:
		for _, item := range t {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

func isBool(v interface{}) bool { _, ok := v.(bool); return ok }

func toNumber(v interface{}) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		return n, err == nil
	}
	return 0, false
}

func toNonNegativeNumber(v interface{}) (int, bool) {
	n, ok := toNumber(v)
	if !ok || n < 0 {
		return 0, false
	}
	return n, true
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func valueError(scope string, idx int, key string, message string) ValidationError {
	return ValidationError{
		Field:   fmt.Sprintf("%s[%d].Config.%s", scope, idx, key),
		Code:    CodeInvalidPluginConfig,
		Message: message,
	}
}

// --- small type helpers for plugin contract validation ---

// --- route conflict detection (method+path overlap between routes) ---

func validateRouteConflicts(cfg *config.Config) ValidationErrors {
	// method → path → first route that registered it
	registry := make(map[string]map[string]int, 8)
	var errs ValidationErrors
	seenConflict := make(map[string]bool)
	for i := range cfg.Routes {
		r := &cfg.Routes[i]
		for _, method := range r.Methods {
			for _, p := range r.Paths {
				pathIdx, ok := registry[method][p]
				if !ok {
					if _, hasPath := registry[method]; !hasPath {
						registry[method] = make(map[string]int)
					}
					registry[method][p] = i
					continue
				}
				key := strings.ToLower(method) + " " + p
				if seenConflict[key] {
					continue
				}
				seenConflict[key] = true
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("Routes[%d]", i),
					Code:    CodeDuplicate,
					Message: fmt.Sprintf("route conflict: %s %s already used by route %q (id: %s)", method, p, cfg.Routes[pathIdx].Name, cfg.Routes[pathIdx].Id),
				})
			}
		}
	}
	return errs
}

func fieldAt(scope string, idx int, name string) string {
	return fmt.Sprintf("%s[%d].%s", scope, idx, name)
}

// validateHealthCheckInterval is exported for future scheduler use (Phase 8):
// the interval string must parse as a positive Go duration.
func validateHealthCheckInterval(interval string) error {
	if interval == "" {
		return nil
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		return fmt.Errorf("health_check interval %q is not a valid duration (e.g. 10s)", interval)
	}
	if d <= 0 {
		return fmt.Errorf("health_check interval %q must be positive", interval)
	}
	return nil
}
