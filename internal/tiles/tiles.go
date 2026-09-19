// Package tiles provides OGC API - Tiles support for neoserver.
package tiles

import (
	"log/slog"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/workspace"
)

// WorkspaceDependencies contains dependencies for workspace-aware handlers.
type WorkspaceDependencies struct {
	Config   conf.Config
	Logger   *slog.Logger
	Registry *workspace.Registry
	Cache    *cache.Manager
	Engine   *Engine
}
