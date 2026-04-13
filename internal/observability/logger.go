package observability

import "go.uber.org/zap"

// NewLogger returns a production-ready zap logger for service startup.
func NewLogger() (*zap.Logger, error) {
	return zap.NewProduction()
}
