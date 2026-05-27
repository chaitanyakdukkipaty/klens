package kinds

// YAMLAppliedMsg is emitted after a successful apply via Applier. The model
// reads this to stash the original YAML for ctrl+z rollback.
type YAMLAppliedMsg struct {
	Kind      string
	Name      string
	Namespace string
}

// YAMLApplyErrMsg is emitted when the apply path fails — bad YAML, forbidden
// patch, or any other error. The model surfaces Err on the status bar and
// returns to the editor.
type YAMLApplyErrMsg struct{ Err error }
