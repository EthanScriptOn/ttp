package store

import (
	"context"
	"strings"
)

// Open selects durable MySQL storage when a DSN is configured. Demo mode is a
// deliberate local fallback so the UI can be explored before infrastructure
// is ready; production deployments should set demo=false.
func Open(ctx context.Context, dsn string, demo bool, bootstrapAdminPassword, demoAdminPassword string) (Store, error) {
	if strings.TrimSpace(dsn) != "" {
		mysqlStore, err := NewMySQL(ctx, dsn, bootstrapAdminPassword)
		if err == nil {
			return mysqlStore, nil
		}
		if !demo {
			return nil, err
		}
	}
	if demo {
		return NewMemoryWithAdminPassword(demoAdminPassword), nil
	}
	return nil, ErrInvalidInput
}
