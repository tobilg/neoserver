package identity

import "context"

// AuditPrincipal is a request-scoped handoff to outer result capture. It holds
// only verified, non-secret attribution, never tokens, claims or session hashes.
type AuditPrincipal struct{ Subject, Method, CredentialID string }
type auditPrincipalKey struct{}

func CaptureAuditPrincipal(ctx context.Context) (context.Context, *AuditPrincipal) {
	value := &AuditPrincipal{}
	return context.WithValue(ctx, auditPrincipalKey{}, value), value
}

// RecordAuditPrincipal is called only after successful authentication (including
// login handlers which create an identity after middleware has already run).
func RecordAuditPrincipal(ctx context.Context, principal *Identity) {
	if value, ok := ctx.Value(auditPrincipalKey{}).(*AuditPrincipal); ok && principal != nil {
		*value = AuditPrincipal{Subject: principal.Subject, Method: string(principal.AuthMethod), CredentialID: principal.APIKeyID}
	}
}
