package devicetrust

import "testing"

func TestInsecureLocalOnlyMutationAuthorizerIsExplicitAndLocalScoped(t *testing.T) {
	authorizer := NewInsecureLocalOnlyMutationAuthorizer()
	if authorizer == nil {
		t.Fatal("local authority is nil")
	}
	if err := authorizer.AuthorizeCommit("", 0, IntentSessionCreate); err != nil {
		t.Fatalf("empty local identity rejected: %v", err)
	}
	if err := authorizer.AuthorizeCommit("device", 0, IntentSessionCreate); err == nil {
		t.Fatal("device identity accepted by local authority")
	}
	if err := authorizer.AuthorizeCommit("", 1, IntentSessionCreate); err == nil {
		t.Fatal("nonzero epoch accepted by local authority")
	}
}
