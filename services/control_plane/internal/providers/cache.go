// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	"context"
	"strings"

	redisapi "github.com/redis/go-redis/v9"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/cache"
	"go.mindclade.dev/libs/go/storage/cache/redis"
	"go.mindclade.dev/services/control_plane/internal/config"
)

// newCacheStore builds the Redis adapter and the component that owns the
// client. Unlike the blob client, the cache is probed: a control-plane read
// path that silently loses its cache degrades into a database overload, so
// reachability is part of readiness rather than discovered on first use.
func newCacheStore(settings config.Settings) (cache.Store, servicekit.Component, error) {
	address := strings.TrimSpace(settings.CacheAddress)
	if address == "" {
		return nil, servicekit.Component{}, faults.New(
			faults.CodeFailedPrecondition,
			"control-plane cache address is not configured",
			faults.WithReason("cache_address_not_configured"),
			faults.WithOperation("controlplane.providers.newCacheStore"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	client := redisapi.NewClient(&redisapi.Options{
		Addr:     address,
		Password: settings.CachePassword,
	})
	options := make([]redis.Option, 0, 1)
	if prefix := strings.TrimSpace(settings.CachePrefix); prefix != "" {
		options = append(options, redis.WithPrefix(prefix))
	}
	store, err := redis.New(client, options...)
	if err != nil {
		_ = client.Close()
		return nil, servicekit.Component{}, err
	}
	probe := func(ctx context.Context) error {
		if err := client.Ping(ctx).Err(); err != nil {
			return faults.Wrap(err, faults.CodeUnavailable,
				"control-plane cache is unreachable",
				faults.WithReason("cache_unreachable"),
				faults.WithOperation("controlplane.providers.cacheComponent.Probe"),
				faults.WithRetryPolicy(faults.BackoffRetry(3)),
			)
		}
		return nil
	}
	component := servicekit.Component{
		Name:      "redis-client",
		Start:     probe,
		Readiness: probe,
		Stop: func(context.Context) error {
			if err := client.Close(); err != nil {
				return faults.Wrap(err, faults.CodeInternal,
					"unable to close the control-plane cache client",
					faults.WithReason("cache_client_close_failed"),
					faults.WithOperation("controlplane.providers.cacheComponent.Stop"),
				)
			}
			return nil
		},
	}
	return store, component, nil
}
