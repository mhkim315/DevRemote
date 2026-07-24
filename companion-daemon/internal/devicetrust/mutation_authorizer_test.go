package devicetrust

type testMutationAuthorizer struct{}

func (testMutationAuthorizer) AuthorizeCommit(string, uint64, MutationIntent) error { return nil }
