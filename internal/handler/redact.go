package handler

import (
	"strings"

	"github.com/haiser1/go-api-gateway/internal/config"
)

const redactedValue = "***REDACTED***"

func redactConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	for i := range cfg.GlobalPlugins {
		redactPluginConfig(cfg.GlobalPlugins[i].Config)
	}
	for i := range cfg.Services {
		for j := range cfg.Services[i].Plugins {
			redactPluginConfig(cfg.Services[i].Plugins[j].Config)
		}
	}
	for i := range cfg.Routes {
		for j := range cfg.Routes[i].Plugins {
			redactPluginConfig(cfg.Routes[i].Plugins[j].Config)
		}
	}
}

func redactPluginConfig(values map[string]interface{}) {
	for key, value := range values {
		if isSecretKey(strings.ToLower(key)) {
			if _, isString := value.(string); isString {
				values[key] = redactedValue
				continue
			}
		}
		values[key] = redactValue(value)
	}
}

func redactValue(value interface{}) interface{} {
	switch nested := value.(type) {
	case map[string]interface{}:
		redactPluginConfig(nested)
		return nested
	case map[interface{}]interface{}:
		for key, item := range nested {
			if keyString, ok := key.(string); ok && isSecretKey(strings.ToLower(strings.TrimSpace(keyString))) {
				if _, isString := item.(string); isString {
					nested[key] = redactedValue
					continue
				}
			}
			nested[key] = redactValue(item)
		}
		return nested
	case []interface{}:
		for i := range nested {
			nested[i] = redactValue(nested[i])
		}
		return nested
	default:
		return value
	}
}

func isSecretKey(key string) bool {
	if strings.HasSuffix(key, "_ref") {
		return false
	}
	switch key {
	case "jwt_key", "api_key", "secret", "token", "password", "private_key":
		return true
	default:
		return false
	}
}
