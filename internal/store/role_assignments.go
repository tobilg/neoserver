package store

import (
	"context"
	"fmt"
)

// Caller holds roleMu for the full assignment write. Catalogs have a single
// active owner, so role deletion cannot race an in-process publication/login.
func (s *DuckDBStore) checkRoleAssignments(ctx context.Context, roles []string) error {
	for _, id := range roles {
		if _, err := s.GetRole(ctx, id); err != nil {
			return fmt.Errorf("cannot assign role %q: %w", id, err)
		}
	}
	return nil
}
