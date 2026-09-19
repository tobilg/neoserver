package identity

import (
	"reflect"
	"strings"
	"testing"
)

func TestExtractMappableClaimsProviderShapes(t *testing.T) {
	tests := []struct {
		name       string
		claims     map[string]interface{}
		configured []string
		claimName  string
		want       []string
	}{
		{name: "Cognito", claims: map[string]interface{}{"cognito:groups": []interface{}{"geo-admins"}}, claimName: "cognito:groups", want: []string{"geo-admins"}},
		{name: "Clerk", claims: map[string]interface{}{"groups": []interface{}{"org:geo"}}, claimName: "groups", want: []string{"org:geo"}},
		{name: "Keycloak", claims: map[string]interface{}{"realm_access": map[string]interface{}{"roles": []interface{}{"workspace-admin"}}}, claimName: "realm_access.roles", want: []string{"workspace-admin"}},
		{name: "Entra", claims: map[string]interface{}{"groups": []interface{}{"0d9f-guid"}}, claimName: "groups", want: []string{"0d9f-guid"}},
		{name: "Auth0 namespaced", claims: map[string]interface{}{"https://geo.example/roles": []interface{}{"publisher"}}, configured: []string{"https://geo.example/roles"}, claimName: "https://geo.example/roles", want: []string{"publisher"}},
		{name: "Okta", claims: map[string]interface{}{"groups": "gis-operators"}, claimName: "groups", want: []string{"gis-operators"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractMappableClaims(test.claims, test.configured)
			if !reflect.DeepEqual(got[test.claimName], test.want) {
				t.Fatalf("claim %q = %v, want %v", test.claimName, got[test.claimName], test.want)
			}
		})
	}
}

func TestExtractClaimValuesPrefersExactFlatName(t *testing.T) {
	claims := map[string]interface{}{
		"realm_access.roles": []interface{}{"flat-role"},
		"realm_access":       map[string]interface{}{"roles": []interface{}{"nested-role"}},
	}
	got := extractClaimValues(claims, "realm_access.roles")
	if !reflect.DeepEqual(got, []string{"flat-role"}) {
		t.Fatalf("got %v, want exact flat claim", got)
	}
}

func TestOIDCAccessScopeValidationFailsClosed(t *testing.T) {
	validator := &OIDCValidator{config: OIDCConfig{RequiredScopes: []string{"openid", "profile"}}}
	for _, claims := range []map[string]interface{}{{"sub": "user"}, {"scope": 7}, {"scope": ""}, {"scp": []interface{}{3}}} {
		if err := validator.validateScopes(claims); err == nil {
			t.Fatalf("missing/malformed access scopes accepted: %v", claims)
		}
	}
	if err := validator.validateScopes(map[string]interface{}{"scope": "openid"}); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("insufficient presented scope error = %v", err)
	}
}
