package panels

// DragSelection holds the lifecycle state for a drag-to-copy interaction.
// Three panels embed it: the log viewer's per-group state, the YAML viewer,
// and the resource table. Each panel feeds it panel-specific
// coordinate→index mappings (line indices, row indices); the lifecycle
// (begin/track/extend/reset) and inclusive range math are shared.
//
// Panels still own their tick command, viewport bounds, scroll mechanism,
// render integration, and clipboard payload — those are genuinely
// panel-specific. Only the state machine common to all three is here.
type DragSelection struct {
	Active bool // mouse button held; true between Begin and Reset
	Moved  bool // cursor moved from its anchor position since Begin
	Start  int  // index into the panel's content units
	End    int  // index, may be < Start
	StartX int  // anchor X at Begin, panel-local coords
	StartY int  // anchor Y at Begin, panel-local coords
	LastX  int  // most recent drag X in panel-local coords
	LastY  int  // most recent drag Y in panel-local coords
}

// NewDragSelection returns an idle selection with Start=End=-1.
func NewDragSelection() DragSelection {
	return DragSelection{Start: -1, End: -1}
}

// Range returns the inclusive [lo, hi] index range, ordered ascending.
// Caller is responsible for checking Active before relying on the indices.
func (d DragSelection) Range() (lo, hi int) {
	lo, hi = d.Start, d.End
	if lo > hi {
		lo, hi = hi, lo
	}
	return
}

// Begin starts a new drag anchored at content index idx with cursor at
// (x, y) in panel-local coords.
func (d *DragSelection) Begin(idx, x, y int) {
	d.Active = true
	d.Moved = false
	d.Start = idx
	d.End = idx
	d.StartX = x
	d.StartY = y
	d.LastX = x
	d.LastY = y
}

// Track records the latest drag cursor position without changing the
// selection range. Used so auto-scroll can see cursor positions outside the
// content area, where Extend would otherwise have nothing to do.
func (d *DragSelection) Track(x, y int) {
	d.LastX = x
	d.LastY = y
	if x != d.StartX || y != d.StartY {
		d.Moved = true
	}
}

// Extend updates End to idx if it differs from the current End. Returns
// true on change and sets Moved so a subsequent click-without-drag can be
// distinguished from a real drag.
func (d *DragSelection) Extend(idx int) bool {
	if idx == d.End {
		return false
	}
	d.Moved = true
	d.End = idx
	return true
}

// Reset clears the drag back to idle.
func (d *DragSelection) Reset() {
	d.Active = false
	d.Moved = false
	d.Start = -1
	d.End = -1
}
