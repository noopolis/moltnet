package store

import "context"

type HealthChecker interface {
	Health(context.Context) error
}
