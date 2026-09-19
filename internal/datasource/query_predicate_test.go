package datasource

import "testing"

func TestRebaseCompiledPredicateParameters(t *testing.T) {
	sql, err := rebaseParameters(`t."$1" = $1 AND t."other" IN ($10, $2) AND '$2' = '$2'`, 1, 9, 10)
	if err != nil || sql != `t."$1" = $9 AND t."other" IN ($18, $10) AND '$2' = '$2'` {
		t.Fatalf("rebase: %s %v", sql, err)
	}
	if _, err := rebaseParameters("$2", 1, 1, 1); err == nil {
		t.Fatal("unbound parameter accepted")
	}
}
