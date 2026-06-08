package layout

import "testing"

func TestDockRectAndShrink(t *testing.T) {
	l := New(100, 40)
	base := l.Content()

	dockH := l.DockHeightFor(false)
	if dockH < minDockHeight {
		t.Fatalf("DockHeightFor = %d, want ≥ %d", dockH, minDockHeight)
	}
	l = l.WithDockHeight(dockH)

	d := l.Dock()
	if d.Width != 100 || d.Height != dockH {
		t.Fatalf("Dock = %+v, want full width × %d", d, dockH)
	}
	// Band sits directly above the status bar.
	if got, want := d.Y+d.Height, 40-statusHeight; got != want {
		t.Fatalf("dock bottom = %d, want %d", got, want)
	}
	// Content gives up exactly the dock height.
	if got, want := l.Content().Height, base.Height-dockH; got != want {
		t.Fatalf("content height = %d, want %d", got, want)
	}
	// Nav bottom must not overlap the dock.
	n := l.Nav()
	if n.Y+n.Height > d.Y {
		t.Fatalf("nav (ends %d) overlaps dock (starts %d)", n.Y+n.Height, d.Y)
	}
}

func TestDockMaximizedFillsMiddle(t *testing.T) {
	l := New(100, 40).WithDockHeight(New(100, 40).DockHeightFor(true))
	d := l.Dock()
	if d.Y != headerHeight || d.Height != 40-headerHeight-statusHeight {
		t.Fatalf("maximized dock = %+v, want the full middle band", d)
	}
}

func TestDockHeightClampsOnShortTerminal(t *testing.T) {
	l := New(80, MinTermHeight)
	dockH := l.DockHeightFor(false)
	middleH := MinTermHeight - headerHeight - statusHeight
	if dockH > middleH-minContentH {
		t.Fatalf("dock %d leaves content < %d rows (middle %d)", dockH, minContentH, middleH)
	}
}

func TestNoDockNoChange(t *testing.T) {
	if d := New(100, 40).Dock(); d.Width != 0 || d.Height != 0 {
		t.Fatalf("Dock with no height = %+v, want zero", d)
	}
}
