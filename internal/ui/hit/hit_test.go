package hit

import (
	"testing"

	"github.com/chaitanyak/klens/internal/ui/layout"
)

// Mirror of the app's non-fullscreen hit map at 100×40: header row 0,
// nav 22 cols below it, tab bar row 1 right of nav, content below the tab
// bar, status on the last row.
func appMap() Map {
	l := layout.New(100, 40)
	var m Map
	m.Add(ZoneHeader, l.Header())
	m.Add(ZoneNav, l.Nav())
	m.Add(ZoneTabBar, l.TabBar())
	m.Add(ZoneContent, l.Content())
	m.Add(ZoneStatus, l.Status())
	return m
}

func TestAtResolvesZonesAndLocalCoords(t *testing.T) {
	m := appMap()
	tests := []struct {
		name   string
		x, y   int
		zone   Zone
		lx, ly int
	}{
		{"header origin", 0, 0, ZoneHeader, 0, 0},
		{"header right edge", 99, 0, ZoneHeader, 99, 0},
		{"nav first cell", 0, 1, ZoneNav, 0, 0},
		{"nav body", 5, 10, ZoneNav, 5, 9},
		{"nav right edge", 21, 10, ZoneNav, 21, 9},
		{"tab bar first cell", 22, 1, ZoneTabBar, 0, 0},
		{"tab bar right edge", 99, 1, ZoneTabBar, 77, 0},
		{"content first cell", 22, 2, ZoneContent, 0, 0},
		{"content body", 60, 20, ZoneContent, 38, 18},
		{"content last row above status", 60, 38, ZoneContent, 38, 36},
		{"status row", 50, 39, ZoneStatus, 50, 0},
	}
	for _, tt := range tests {
		zone, lx, ly := m.At(tt.x, tt.y)
		if zone != tt.zone || lx != tt.lx || ly != tt.ly {
			t.Errorf("%s: At(%d,%d) = (%v,%d,%d), want (%v,%d,%d)",
				tt.name, tt.x, tt.y, zone, lx, ly, tt.zone, tt.lx, tt.ly)
		}
	}
}

func TestAtOutsideAllRegions(t *testing.T) {
	m := appMap()
	if zone, _, _ := m.At(150, 50); zone != ZoneNone {
		t.Errorf("At outside terminal = %v, want ZoneNone", zone)
	}
	if zone, _, _ := m.At(-1, 5); zone != ZoneNone {
		t.Errorf("At negative X = %v, want ZoneNone", zone)
	}
}

func TestZeroAreaRegionsSkipped(t *testing.T) {
	var m Map
	m.Add(ZoneTabBar, layout.Rect{X: 0, Y: 0, Width: 0, Height: 1})
	m.Add(ZoneContent, layout.Rect{X: 0, Y: 0, Width: 10, Height: 10})
	if zone, _, _ := m.At(0, 0); zone != ZoneContent {
		t.Errorf("zero-area region not skipped: got %v", zone)
	}
}

func TestFirstMatchWins(t *testing.T) {
	var m Map
	m.Add(ZoneTabBar, layout.Rect{X: 0, Y: 0, Width: 10, Height: 1})
	m.Add(ZoneContent, layout.Rect{X: 0, Y: 0, Width: 10, Height: 10})
	if zone, _, _ := m.At(5, 0); zone != ZoneTabBar {
		t.Errorf("overlap: got %v, want first-added ZoneTabBar", zone)
	}
}

func TestRectOf(t *testing.T) {
	m := appMap()
	nav := m.RectOf(ZoneNav)
	if nav.X != 0 || nav.Y != 1 || nav.Width != 22 {
		t.Errorf("RectOf(ZoneNav) = %+v", nav)
	}
	if got := m.RectOf(ZoneNone); got != (layout.Rect{}) {
		t.Errorf("RectOf(absent) = %+v, want zero", got)
	}
}
