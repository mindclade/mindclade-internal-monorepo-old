// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package config defines the control-plane process configuration schema and
// translates the transport-neutral libs/go/config snapshot into typed service
// settings. It owns no environment reads outside the explicit EnvSource map and
// no provider construction.
package config

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	foundationconfig "go.mindclade.dev/libs/go/config"
	"go.mindclade.dev/libs/go/faults"
)

// Environment identifies the operational qualification boundary for a
// process. Production and staging fail closed on missing durable-provider and
// signing configuration; development and test may use explicitly selected
// in-memory adapters.
type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

func (environment Environment) Valid() bool {
	switch environment {
	case EnvironmentDevelopment, EnvironmentTest, EnvironmentStaging, EnvironmentProduction:
		return true
	default:
		return false
	}
}

// Settings is the typed, validated configuration consumed by a process-owned
// provider factory. Secrets remain available only to the factory; diagnostics
// should use the underlying Snapshot.Redacted representation.
type Settings struct {
	Environment    Environment
	ServiceName    string
	HTTPAddress    string
	GRPCAddress    string
	MetricsAddress string
	LogLevel       string

	ShutdownTimeout time.Duration
	DrainTimeout    time.Duration

	DatabaseDriver    string
	DatabaseDSN       string
	DatabaseMaxOpen   int
	DatabaseMaxIdle   int
	MigrationsEnabled bool

	MessagingProvider     string
	MessagingTopic        string
	MessagingSubscription string

	// Blob and cache settings are provider-required rather than
	// process-required: only roles whose production profile demands
	// CapabilityBlobStore or CapabilityCache read them, and those factories
	// fail closed when a value is absent.
	BlobBucket    string
	BlobPrefix    string
	CacheAddress  string
	CachePassword string
	CachePrefix   string

	// Kubernetes settings are provider-required in the same sense: only the
	// scheduler, controller, operator, and ingestion-coordinator profiles
	// demand CapabilityKubernetes. The default source discovers an in-cluster
	// configuration and falls back to a kubeconfig, so a deployed pod needs no
	// setting at all and a developer needs only a path.
	KubernetesSource     string
	KubernetesKubeconfig string
	KubernetesContext    string
	KubernetesTimeout    time.Duration

	// gRPC transport security. When a certificate and key are configured the
	// server terminates TLS itself; when they are absent it serves plaintext
	// and the deployment is expected to terminate TLS ahead of the process.
	// A client CA turns on mutual TLS and is only meaningful with the pair.
	GRPCTLSCertificate string
	GRPCTLSKey         string
	GRPCTLSClientCA    string

	SigningKeyID   string
	SigningHMACKey string

	// AuthAPIKeys carries the service-to-service credential registry in the
	// form "subject:sha256hex:permission[,permission]" with entries separated
	// by ";". Only roles whose production profile demands
	// CapabilityAuthentication read it, and those factories fail closed when
	// it is empty.
	AuthAPIKeys     string
	AuthIAPAudience string
	PaginationTTL   time.Duration

	OutboundAllowedHosts []string
}

// Resolved keeps the typed settings, immutable source snapshot, and reload
// holder together so configuration evidence cannot drift from the process.
type Resolved struct {
	Settings Settings
	Snapshot foundationconfig.Snapshot
	Current  *foundationconfig.Atomic
}

// Schema returns the canonical control-plane configuration contract.
func Schema(serviceName string) []foundationconfig.Field {
	serviceName = strings.TrimSpace(serviceName)
	return []foundationconfig.Field{
		{Key: "service.name", Required: true, Default: foundationconfig.String(serviceName), Validate: nonEmpty("service.name")},
		{Key: "environment", Required: true, Default: foundationconfig.String(string(EnvironmentDevelopment)), Validate: validEnvironment},
		{Key: "http.address", Default: foundationconfig.String(":8080"), Validate: validListenAddress("http.address")},
		{Key: "grpc.address", Default: foundationconfig.String(":9090"), Validate: validListenAddress("grpc.address")},
		{Key: "metrics.address", Default: foundationconfig.String(":9464"), Validate: validListenAddress("metrics.address")},
		{Key: "log.level", Default: foundationconfig.String("info"), Reloadable: true, Validate: oneOf("log.level", "debug", "info", "warn", "error")},
		{Key: "shutdown.timeout", Default: foundationconfig.String("30s"), Validate: positiveDuration("shutdown.timeout")},
		{Key: "drain.timeout", Default: foundationconfig.String("20s"), Validate: positiveDuration("drain.timeout")},
		{Key: "database.driver", Default: foundationconfig.String("postgres"), Validate: nonEmpty("database.driver")},
		{Key: "database.dsn", Secret: true},
		{Key: "database.max_open", Default: foundationconfig.String("32"), Validate: nonNegativeInteger("database.max_open")},
		{Key: "database.max_idle", Default: foundationconfig.String("8"), Validate: nonNegativeInteger("database.max_idle")},
		{Key: "migrations.enabled", Default: foundationconfig.String("true"), Validate: boolean("migrations.enabled")},
		{Key: "messaging.provider", Default: foundationconfig.String("memory"), Validate: oneOf("messaging.provider", "memory", "pubsub")},
		{Key: "messaging.topic", Default: foundationconfig.String("mindclade.control.events"), Validate: nonEmpty("messaging.topic")},
		{Key: "messaging.subscription"},
		{Key: "blob.bucket"},
		{Key: "blob.prefix"},
		{Key: "cache.address"},
		{Key: "cache.password", Secret: true},
		{Key: "cache.prefix"},
		{Key: "kubernetes.source", Default: foundationconfig.String("auto"), Validate: oneOf("kubernetes.source", "auto", "in_cluster", "kubeconfig")},
		{Key: "kubernetes.kubeconfig"},
		{Key: "kubernetes.context"},
		{Key: "kubernetes.timeout", Default: foundationconfig.String("30s"), Validate: positiveDuration("kubernetes.timeout")},
		{Key: "grpc.tls.certificate"},
		{Key: "grpc.tls.key", Secret: true},
		{Key: "grpc.tls.client_ca"},
		{Key: "signing.key_id", Default: foundationconfig.String("development/control-plane")},
		{Key: "signing.hmac_key", Secret: true},
		{Key: "auth.api_keys", Secret: true},
		{Key: "auth.iap_audience"},
		{Key: "pagination.ttl", Default: foundationconfig.String("15m"), Validate: positiveDuration("pagination.ttl")},
		{Key: "outbound.allowed_hosts", Default: foundationconfig.String("")},
	}
}

// EnvironmentSource maps only known logical fields to exact process
// environment variables. It deliberately never scans the ambient environment.
func EnvironmentSource() foundationconfig.EnvSource {
	return foundationconfig.EnvSource{SourceName: "environment", Mapping: map[string]string{
		"service.name":           "MINDCLADE_SERVICE_NAME",
		"environment":            "MINDCLADE_ENVIRONMENT",
		"http.address":           "MINDCLADE_HTTP_ADDRESS",
		"grpc.address":           "MINDCLADE_GRPC_ADDRESS",
		"metrics.address":        "MINDCLADE_METRICS_ADDRESS",
		"log.level":              "MINDCLADE_LOG_LEVEL",
		"shutdown.timeout":       "MINDCLADE_SHUTDOWN_TIMEOUT",
		"drain.timeout":          "MINDCLADE_DRAIN_TIMEOUT",
		"database.driver":        "MINDCLADE_DATABASE_DRIVER",
		"database.dsn":           "MINDCLADE_DATABASE_DSN",
		"database.max_open":      "MINDCLADE_DATABASE_MAX_OPEN",
		"database.max_idle":      "MINDCLADE_DATABASE_MAX_IDLE",
		"migrations.enabled":     "MINDCLADE_MIGRATIONS_ENABLED",
		"messaging.provider":     "MINDCLADE_MESSAGING_PROVIDER",
		"messaging.topic":        "MINDCLADE_MESSAGING_TOPIC",
		"messaging.subscription": "MINDCLADE_MESSAGING_SUBSCRIPTION",
		"blob.bucket":            "MINDCLADE_BLOB_BUCKET",
		"blob.prefix":            "MINDCLADE_BLOB_PREFIX",
		"cache.address":          "MINDCLADE_CACHE_ADDRESS",
		"cache.password":         "MINDCLADE_CACHE_PASSWORD",
		"cache.prefix":           "MINDCLADE_CACHE_PREFIX",
		"kubernetes.source":      "MINDCLADE_KUBERNETES_SOURCE",
		"kubernetes.kubeconfig":  "MINDCLADE_KUBERNETES_KUBECONFIG",
		"kubernetes.context":     "MINDCLADE_KUBERNETES_CONTEXT",
		"kubernetes.timeout":     "MINDCLADE_KUBERNETES_TIMEOUT",
		"grpc.tls.certificate":   "MINDCLADE_GRPC_TLS_CERTIFICATE",
		"grpc.tls.key":           "MINDCLADE_GRPC_TLS_KEY",
		"grpc.tls.client_ca":     "MINDCLADE_GRPC_TLS_CLIENT_CA",
		"signing.key_id":         "MINDCLADE_SIGNING_KEY_ID",
		"signing.hmac_key":       "MINDCLADE_SIGNING_HMAC_KEY",
		"auth.api_keys":          "MINDCLADE_AUTH_API_KEYS",
		"auth.iap_audience":      "MINDCLADE_AUTH_IAP_AUDIENCE",
		"pagination.ttl":         "MINDCLADE_PAGINATION_TTL",
		"outbound.allowed_hosts": "MINDCLADE_OUTBOUND_ALLOWED_HOSTS",
	}}
}

// Load resolves sources in caller order after defaults and converts the result
// to Settings. The environment source should normally be the last source.
func Load(ctx context.Context, serviceName string, sources ...foundationconfig.Source) (Resolved, error) {
	if ctx == nil || strings.TrimSpace(serviceName) == "" {
		return Resolved{}, invalid("invalid_config_load_request", "controlplane.config.Load", nil)
	}
	loader, err := foundationconfig.New(Schema(serviceName), sources...)
	if err != nil {
		return Resolved{}, err
	}
	snapshot, err := loader.Load(ctx)
	if err != nil {
		return Resolved{}, err
	}
	settings, err := Decode(snapshot)
	if err != nil {
		return Resolved{}, err
	}
	current, err := foundationconfig.NewAtomic(snapshot)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Settings: settings, Snapshot: snapshot, Current: current}, nil
}

// Decode validates and converts one resolved snapshot.
func Decode(snapshot foundationconfig.Snapshot) (Settings, error) {
	if snapshot.Digest().IsZero() {
		return Settings{}, invalid("empty_config_snapshot", "controlplane.config.Decode", nil)
	}
	settings := Settings{
		Environment:           Environment(snapshot.MustGet("environment")),
		ServiceName:           snapshot.MustGet("service.name"),
		HTTPAddress:           snapshot.MustGet("http.address"),
		GRPCAddress:           snapshot.MustGet("grpc.address"),
		MetricsAddress:        snapshot.MustGet("metrics.address"),
		LogLevel:              snapshot.MustGet("log.level"),
		DatabaseDriver:        snapshot.MustGet("database.driver"),
		DatabaseDSN:           value(snapshot, "database.dsn"),
		MessagingProvider:     snapshot.MustGet("messaging.provider"),
		MessagingTopic:        snapshot.MustGet("messaging.topic"),
		MessagingSubscription: value(snapshot, "messaging.subscription"),
		BlobBucket:            value(snapshot, "blob.bucket"),
		BlobPrefix:            value(snapshot, "blob.prefix"),
		CacheAddress:          value(snapshot, "cache.address"),
		CachePassword:         value(snapshot, "cache.password"),
		CachePrefix:           value(snapshot, "cache.prefix"),
		KubernetesSource:      snapshot.MustGet("kubernetes.source"),
		KubernetesKubeconfig:  value(snapshot, "kubernetes.kubeconfig"),
		KubernetesContext:     value(snapshot, "kubernetes.context"),
		GRPCTLSCertificate:    value(snapshot, "grpc.tls.certificate"),
		GRPCTLSKey:            value(snapshot, "grpc.tls.key"),
		GRPCTLSClientCA:       value(snapshot, "grpc.tls.client_ca"),
		SigningKeyID:          snapshot.MustGet("signing.key_id"),
		SigningHMACKey:        value(snapshot, "signing.hmac_key"),
		AuthAPIKeys:           value(snapshot, "auth.api_keys"),
		AuthIAPAudience:       value(snapshot, "auth.iap_audience"),
		OutboundAllowedHosts:  splitCSV(value(snapshot, "outbound.allowed_hosts")),
	}
	var err error
	if settings.ShutdownTimeout, err = time.ParseDuration(snapshot.MustGet("shutdown.timeout")); err != nil {
		return Settings{}, invalid("invalid_shutdown_timeout", "controlplane.config.Decode", faults.Fields{"cause": err.Error()})
	}
	if settings.KubernetesTimeout, err = time.ParseDuration(snapshot.MustGet("kubernetes.timeout")); err != nil {
		return Settings{}, invalid("invalid_kubernetes_timeout", "controlplane.config.Decode", faults.Fields{"cause": err.Error()})
	}
	if settings.DrainTimeout, err = time.ParseDuration(snapshot.MustGet("drain.timeout")); err != nil {
		return Settings{}, invalid("invalid_drain_timeout", "controlplane.config.Decode", faults.Fields{"cause": err.Error()})
	}
	if settings.PaginationTTL, err = time.ParseDuration(snapshot.MustGet("pagination.ttl")); err != nil {
		return Settings{}, invalid("invalid_pagination_ttl", "controlplane.config.Decode", faults.Fields{"cause": err.Error()})
	}
	if settings.DatabaseMaxOpen, err = strconv.Atoi(snapshot.MustGet("database.max_open")); err != nil {
		return Settings{}, invalid("invalid_database_max_open", "controlplane.config.Decode", nil)
	}
	if settings.DatabaseMaxIdle, err = strconv.Atoi(snapshot.MustGet("database.max_idle")); err != nil {
		return Settings{}, invalid("invalid_database_max_idle", "controlplane.config.Decode", nil)
	}
	if settings.MigrationsEnabled, err = strconv.ParseBool(snapshot.MustGet("migrations.enabled")); err != nil {
		return Settings{}, invalid("invalid_migrations_enabled", "controlplane.config.Decode", nil)
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func value(snapshot foundationconfig.Snapshot, key string) string {
	result, _ := snapshot.Get(key)
	return result
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func validEnvironment(value string) error {
	if !Environment(value).Valid() {
		return invalid("invalid_environment", "controlplane.config.ValidateEnvironment", faults.Fields{"environment": value})
	}
	return nil
}

func validListenAddress(key string) foundationconfig.Validator {
	return func(value string) error {
		if value == "" {
			return invalid("empty_listen_address", "controlplane.config.ValidateAddress", faults.Fields{"key": key})
		}
		if _, err := net.ResolveTCPAddr("tcp", value); err != nil {
			return invalid("invalid_listen_address", "controlplane.config.ValidateAddress", faults.Fields{"key": key})
		}
		return nil
	}
}

func nonEmpty(key string) foundationconfig.Validator {
	return func(value string) error {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return invalid("invalid_nonempty_value", "controlplane.config.Validate", faults.Fields{"key": key})
		}
		return nil
	}
}

func oneOf(key string, allowed ...string) foundationconfig.Validator {
	return func(value string) error {
		for _, candidate := range allowed {
			if value == candidate {
				return nil
			}
		}
		return invalid("unsupported_config_value", "controlplane.config.Validate", faults.Fields{"key": key, "value": value})
	}
}

func positiveDuration(key string) foundationconfig.Validator {
	return func(value string) error {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return invalid("invalid_positive_duration", "controlplane.config.Validate", faults.Fields{"key": key})
		}
		return nil
	}
}

func nonNegativeInteger(key string) foundationconfig.Validator {
	return func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return invalid("invalid_nonnegative_integer", "controlplane.config.Validate", faults.Fields{"key": key})
		}
		return nil
	}
}

func boolean(key string) foundationconfig.Validator {
	return func(value string) error {
		if _, err := strconv.ParseBool(value); err != nil {
			return invalid("invalid_boolean", "controlplane.config.Validate", faults.Fields{"key": key})
		}
		return nil
	}
}
