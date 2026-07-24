package devicetrust

type testMutationAuthorizer struct{}

func (testMutationAuthorizer) AuthorizeCommit(string, uint64, MutationIntent) error { return nil }
func (a testMutationAuthorizer) AuthorizeAndCommit(_ string, _ uint64, _ MutationIntent, commit func() error) error {
	return commit()
}
