package store

import (
	"context"
	"fmt"
	"strings"
)

// Open always returns durable storage. A missing or unavailable database is a
// startup error; silently switching to an in-memory store would make releases
// disappear on restart.
func Open(ctx context.Context, dsn string) (Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql dsn is required")
	}
	return NewMySQL(ctx, dsn)
}
