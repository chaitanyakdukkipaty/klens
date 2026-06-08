// Package hit provides the screen hit-region registry used to route mouse
// events. The root model rebuilds a Map from the current layout on every
// mouse event (regions are cheap: a handful of rect appends), then resolves
// the event's screen point to a zone plus zone-local coordinates with At.
//
// This replaces per-call-site offset arithmetic: panels receive coordinates
// relative to their own outer rectangle and keep all interior hit logic
// (rows, tabs, buttons) to themselves.
package hit

import "github.com/chaitanyak/klens/internal/ui/layout"

// Zone identifies a top-level screen region.
type Zone int

const (
	ZoneNone Zone = iota
	ZoneHeader
	ZoneNav
	ZoneTabBar
	ZoneContent
	ZoneStatus
	ZoneDock
)

// Region pairs a zone with its absolute screen rectangle.
type Region struct {
	Zone Zone
	Rect layout.Rect
}

// Map is an ordered hit-region registry; the first containing region wins.
type Map struct {
	regions []Region
}

// Add registers a region. Zero-area rects are skipped so callers can add
// conditionally-present panels unconditionally.
func (m *Map) Add(z Zone, r layout.Rect) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	m.regions = append(m.regions, Region{Zone: z, Rect: r})
}

// At resolves a screen point to its zone and zone-local coordinates.
// Returns ZoneNone when no region contains the point.
func (m Map) At(x, y int) (Zone, int, int) {
	for _, reg := range m.regions {
		if reg.Rect.Contains(x, y) {
			lx, ly := reg.Rect.Local(x, y)
			return reg.Zone, lx, ly
		}
	}
	return ZoneNone, 0, 0
}

// RectOf returns the rect registered for zone z (zero Rect when absent).
func (m Map) RectOf(z Zone) layout.Rect {
	for _, reg := range m.regions {
		if reg.Zone == z {
			return reg.Rect
		}
	}
	return layout.Rect{}
}
