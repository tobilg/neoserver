package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

func tokenDuration(value string) (time.Duration, error) {
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		days, parseErr := strconv.ParseUint(strings.TrimSuffix(value, "d"), 10, 64)
		if parseErr != nil || days == 0 || days > uint64(math.MaxInt64/int64(24*time.Hour)) {
			return 0, fmt.Errorf("invalid expiry; use a positive duration such as 24h or 7d")
		}
		duration = time.Duration(days) * 24 * time.Hour
	} else {
		duration, err = time.ParseDuration(value)
	}
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid expiry; use a positive duration such as 24h or 7d")
	}
	return duration, nil
}

func validTokenRole(role string) bool {
	return role == "super_admin" || role == "admin" || role == "editor" || role == "viewer"
}

// validTokenScope keeps super_admin global and every other role workspace-bound.
func validTokenScope(role, workspace string) error {
	if role == "super_admin" && workspace != "" {
		return errors.New("super_admin tokens apply to all workspaces; omit --workspace")
	}
	if role != "super_admin" && workspace == "" {
		return fmt.Errorf("--workspace is required for role %s", role)
	}
	return nil
}

type workspaceLookup interface {
	GetWorkspace(context.Context, string) (*store.Workspace, error)
	GetWorkspaceByName(context.Context, string) (*store.Workspace, error)
}

// resolveTokenWorkspace accepts a workspace ID or name, as the management API does.
func resolveTokenWorkspace(ctx context.Context, workspaces workspaceLookup, identifier string) (*store.Workspace, error) {
	ws, err := workspaces.GetWorkspace(ctx, identifier)
	if errors.Is(err, store.ErrNotFound) {
		ws, err = workspaces.GetWorkspaceByName(ctx, identifier)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("workspace %q not found", identifier)
	}
	return ws, err
}
