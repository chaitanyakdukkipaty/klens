package app

// CloseModeMsg asks the root to return to the resource table.
// A controller emits this when the user has fully exited its mode (i.e. every
// layered state it owns has already been peeled).
type CloseModeMsg struct{}
