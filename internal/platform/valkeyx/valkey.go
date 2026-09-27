// Package valkeyx wraps the Valkey client used for cache, presence and permission invalidation.
package valkeyx

import (
	"context"
	"fmt"

	"github.com/valkey-io/valkey-go"
)

func Connect(addr string) (valkey.Client, error) {
	c, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	if err != nil {
		return nil, fmt.Errorf("connect valkey %s: %w", addr, err)
	}
	return c, nil
}

func Ping(ctx context.Context, c valkey.Client) error {
	return c.Do(ctx, c.B().Ping().Build()).Error()
}
