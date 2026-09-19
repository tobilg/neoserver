package rbac

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/tobilg/neoserver/internal/store"
)

// quoteIdent quotes an SQL identifier (table name, column name) to prevent SQL injection.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// Adapter is the interface for Casbin policy storage.
type Adapter interface {
	persist.Adapter
}

type policyStore interface {
	LoadCasbinPolicies(context.Context) ([]*store.CasbinRule, error)
	SaveCasbinPolicy(context.Context, *store.CasbinRule) error
	RemoveCasbinPolicy(context.Context, *store.CasbinRule) error
	ReplaceCasbinPolicies(context.Context, []*store.CasbinRule) error
}

// StoreAdapter persists the active Casbin model in neoserver's encrypted
// catalog. It replaces the former process-only policy adapter used at startup.
type StoreAdapter struct{ store policyStore }

func NewStoreAdapter(value policyStore) *StoreAdapter { return &StoreAdapter{store: value} }

func (a *StoreAdapter) LoadPolicy(target model.Model) error {
	rules, err := a.store.LoadCasbinPolicies(context.Background())
	if err != nil {
		return err
	}
	for _, rule := range rules {
		line := strings.Join([]string{rule.PType, rule.V0, rule.V1, rule.V2, rule.V3}, ", ")
		if err := persist.LoadPolicyLine(line, target); err != nil {
			return err
		}
	}
	return nil
}

func (a *StoreAdapter) SavePolicy(target model.Model) error {
	var rules []*store.CasbinRule
	for ptype, assertion := range target["p"] {
		for _, values := range assertion.Policy {
			rules = append(rules, casbinRule(ptype, values))
		}
	}
	for ptype, assertion := range target["g"] {
		for _, values := range assertion.Policy {
			rules = append(rules, casbinRule(ptype, values))
		}
	}
	return a.store.ReplaceCasbinPolicies(context.Background(), rules)
}

func (a *StoreAdapter) AddPolicy(_ string, ptype string, rule []string) error {
	return a.store.SaveCasbinPolicy(context.Background(), casbinRule(ptype, rule))
}

func (a *StoreAdapter) RemovePolicy(_ string, ptype string, rule []string) error {
	return a.store.RemoveCasbinPolicy(context.Background(), casbinRule(ptype, rule))
}

func (a *StoreAdapter) RemoveFilteredPolicy(_ string, ptype string, fieldIndex int, fieldValues ...string) error {
	rules, err := a.store.LoadCasbinPolicies(context.Background())
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.PType != ptype {
			continue
		}
		values := []string{rule.V0, rule.V1, rule.V2, rule.V3, rule.V4, rule.V5}
		match := true
		for i, expected := range fieldValues {
			if expected != "" && (fieldIndex+i >= len(values) || values[fieldIndex+i] != expected) {
				match = false
				break
			}
		}
		if match {
			if err := a.store.RemoveCasbinPolicy(context.Background(), rule); err != nil {
				return err
			}
		}
	}
	return nil
}

func casbinRule(ptype string, values []string) *store.CasbinRule {
	result := &store.CasbinRule{PType: ptype}
	targets := []*string{&result.V0, &result.V1, &result.V2, &result.V3, &result.V4, &result.V5}
	for i := range values {
		if i < len(targets) {
			*targets[i] = values[i]
		}
	}
	return result
}

// DuckDBAdapter is a Casbin adapter that uses DuckDB for storage.
type DuckDBAdapter struct {
	db        *sql.DB
	tableName string
}

// NewDuckDBAdapter creates a new DuckDB adapter for Casbin.
func NewDuckDBAdapter(db *sql.DB) *DuckDBAdapter {
	return &DuckDBAdapter{
		db:        db,
		tableName: "casbin_rules",
	}
}

// LoadPolicy loads all policy rules from the database.
func (a *DuckDBAdapter) LoadPolicy(model model.Model) error {
	ctx := context.Background()
	query := fmt.Sprintf(`
		SELECT ptype, v0, v1, v2, v3, v4, v5
		FROM %s
	`, quoteIdent(a.tableName))

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query policies: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ptype, v0, v1, v2, v3, v4, v5 sql.NullString
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3, &v4, &v5); err != nil {
			return fmt.Errorf("failed to scan policy: %w", err)
		}

		line := a.buildLine(ptype.String, v0.String, v1.String, v2.String, v3.String, v4.String, v5.String)
		if err := persist.LoadPolicyLine(line, model); err != nil {
			return fmt.Errorf("failed to load policy line: %w", err)
		}
	}

	return rows.Err()
}

// SavePolicy saves all policy rules to the database.
func (a *DuckDBAdapter) SavePolicy(model model.Model) error {
	ctx := context.Background()

	// Clear existing policies
	if _, err := a.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", quoteIdent(a.tableName))); err != nil {
		return fmt.Errorf("failed to clear policies: %w", err)
	}

	// Insert p policies
	for ptype, ast := range model["p"] {
		for _, rule := range ast.Policy {
			if err := a.insertPolicy(ctx, ptype, rule); err != nil {
				return err
			}
		}
	}

	// Insert g policies (grouping/role inheritance)
	for ptype, ast := range model["g"] {
		for _, rule := range ast.Policy {
			if err := a.insertPolicy(ctx, ptype, rule); err != nil {
				return err
			}
		}
	}

	return nil
}

// AddPolicy adds a policy rule to the database.
func (a *DuckDBAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	ctx := context.Background()
	return a.insertPolicy(ctx, ptype, rule)
}

// RemovePolicy removes a policy rule from the database.
func (a *DuckDBAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	ctx := context.Background()

	args := make([]interface{}, 0, 7)
	args = append(args, ptype)

	conditions := []string{"ptype = ?"}
	for i, v := range rule {
		if v != "" {
			conditions = append(conditions, fmt.Sprintf("v%d = ?", i))
			args = append(args, v)
		}
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(a.tableName), strings.Join(conditions, " AND "))
	_, err := a.db.ExecContext(ctx, query, args...)
	return err
}

// RemoveFilteredPolicy removes policy rules that match the filter.
func (a *DuckDBAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	ctx := context.Background()

	args := make([]interface{}, 0, 7)
	args = append(args, ptype)

	conditions := []string{"ptype = ?"}
	for i, v := range fieldValues {
		if v != "" {
			conditions = append(conditions, fmt.Sprintf("v%d = ?", fieldIndex+i))
			args = append(args, v)
		}
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(a.tableName), strings.Join(conditions, " AND "))
	_, err := a.db.ExecContext(ctx, query, args...)
	return err
}

// insertPolicy inserts a single policy rule.
func (a *DuckDBAdapter) insertPolicy(ctx context.Context, ptype string, rule []string) error {
	values := make([]interface{}, 7)
	values[0] = ptype

	for i := 0; i < 6; i++ {
		if i < len(rule) {
			values[i+1] = rule[i]
		} else {
			values[i+1] = ""
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (ptype, v0, v1, v2, v3, v4, v5)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, quoteIdent(a.tableName))

	_, err := a.db.ExecContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("failed to insert policy: %w", err)
	}
	return nil
}

// buildLine constructs a policy line from values.
func (a *DuckDBAdapter) buildLine(ptype string, values ...string) string {
	parts := []string{ptype}
	for _, v := range values {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}

// MemoryAdapter is an in-memory adapter for testing.
type MemoryAdapter struct {
	policies [][]string
}

// NewMemoryAdapter creates a new in-memory adapter.
func NewMemoryAdapter() *MemoryAdapter {
	return &MemoryAdapter{
		policies: make([][]string, 0),
	}
}

// LoadPolicy loads policies from memory.
func (a *MemoryAdapter) LoadPolicy(model model.Model) error {
	for _, p := range a.policies {
		if len(p) < 1 {
			continue
		}
		line := strings.Join(p, ", ")
		if err := persist.LoadPolicyLine(line, model); err != nil {
			return err
		}
	}
	return nil
}

// SavePolicy saves policies to memory.
func (a *MemoryAdapter) SavePolicy(model model.Model) error {
	a.policies = make([][]string, 0)

	for ptype, ast := range model["p"] {
		for _, rule := range ast.Policy {
			p := make([]string, 0, len(rule)+1)
			p = append(p, ptype)
			p = append(p, rule...)
			a.policies = append(a.policies, p)
		}
	}

	for ptype, ast := range model["g"] {
		for _, rule := range ast.Policy {
			p := make([]string, 0, len(rule)+1)
			p = append(p, ptype)
			p = append(p, rule...)
			a.policies = append(a.policies, p)
		}
	}

	return nil
}

// AddPolicy adds a policy to memory.
func (a *MemoryAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	p := make([]string, 0, len(rule)+1)
	p = append(p, ptype)
	p = append(p, rule...)
	a.policies = append(a.policies, p)
	return nil
}

// RemovePolicy removes a policy from memory.
func (a *MemoryAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	for i, p := range a.policies {
		if len(p) > 0 && p[0] == ptype && a.matchRule(p[1:], rule) {
			a.policies = append(a.policies[:i], a.policies[i+1:]...)
			return nil
		}
	}
	return nil
}

// RemoveFilteredPolicy removes filtered policies from memory.
func (a *MemoryAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	newPolicies := make([][]string, 0, len(a.policies))
	for _, p := range a.policies {
		if len(p) == 0 || p[0] != ptype {
			newPolicies = append(newPolicies, p)
			continue
		}

		match := true
		for i, v := range fieldValues {
			if v != "" && len(p) > fieldIndex+i+1 && p[fieldIndex+i+1] != v {
				match = false
				break
			}
		}
		if !match {
			newPolicies = append(newPolicies, p)
		}
	}
	a.policies = newPolicies
	return nil
}

// matchRule checks if two rules match.
func (a *MemoryAdapter) matchRule(p1, p2 []string) bool {
	if len(p1) != len(p2) {
		return false
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			return false
		}
	}
	return true
}
