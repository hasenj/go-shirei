package shirei

import "testing"

// These validate the measure-only region analysis: region collection, bottom-up
// hashing, and cache-on-second-hit stability, including change propagation up the
// nesting and sibling isolation. See notes/container-cache-plan.md.

func pushS() Surface          { return Surface{Clip: ClipPush} }
func popS() Surface           { return Surface{Clip: ClipPop} }
func fillS(v float32) Surface { return Surface{Color1: Vec4{v, v, v, 1}} }

// runFrame drives one analyze pass and returns the per-frame stats (fetch resets).
func runFrame(rc *regionCache, ss []Surface) RegionStats {
	rc.collectRegions(ss)
	return rc.fetchStats()
}

func TestRegionStabilityFlat(t *testing.T) {
	var rc regionCache
	ss := []Surface{pushS(), fillS(0.5), popS()} // one region, 3 surfaces

	s1 := runFrame(&rc, ss)
	if s1.Regions != 1 || s1.StableRegions != 0 || s1.Surfaces != 3 || s1.Covered != 0 {
		t.Fatalf("frame1: %+v", s1)
	}
	s2 := runFrame(&rc, ss) // identical frame -> stable on the second sighting
	if s2.Regions != 1 || s2.StableRegions != 1 || s2.Covered != 3 {
		t.Fatalf("frame2: %+v", s2)
	}
}

func TestRegionChangePropagatesUp(t *testing.T) {
	var rc regionCache
	// outer[ fillA, inner[ fillB ], fillC ]
	base := []Surface{
		pushS(), fillS(0.1), pushS(), fillS(0.2), popS(), fillS(0.3), popS(),
	}
	runFrame(&rc, base)
	s2 := runFrame(&rc, base)
	if s2.Regions != 2 || s2.StableRegions != 2 || s2.Covered != 7 {
		t.Fatalf("frame2 (all stable): %+v", s2)
	}

	// Change the inner fill: inner hash changes, and it folds up so the outer
	// changes too. Nothing is stable this frame.
	changed := append([]Surface(nil), base...)
	changed[3] = fillS(0.9)
	s3 := runFrame(&rc, changed)
	if s3.Regions != 2 || s3.StableRegions != 0 || s3.Covered != 0 {
		t.Fatalf("frame3 (inner changed -> outer changed): %+v", s3)
	}
}

func TestRegionOuterOnlyChange(t *testing.T) {
	var rc regionCache
	base := []Surface{
		pushS(), fillS(0.1), pushS(), fillS(0.2), popS(), fillS(0.3), popS(),
	}
	runFrame(&rc, base)
	runFrame(&rc, base)

	// Change only an outer direct surface (fillC). The outer region changes, but
	// the inner region's content is untouched -> inner stays stable.
	changed := append([]Surface(nil), base...)
	changed[5] = fillS(0.9)
	s := runFrame(&rc, changed)
	if s.StableRegions != 1 {
		t.Fatalf("expected only the inner region stable, got %+v", s)
	}
	if s.Covered != 3 { // inner direct: pushInner, fillB, popInner
		t.Fatalf("expected covered=3 (inner span), got %d", s.Covered)
	}
}

func TestRegionSiblingIsolation(t *testing.T) {
	var rc regionCache
	// outer[ A[ fillA ], B[ fillB ] ]
	base := []Surface{
		pushS(), pushS(), fillS(0.1), popS(), pushS(), fillS(0.2), popS(), popS(),
	}
	runFrame(&rc, base)
	s2 := runFrame(&rc, base)
	if s2.Regions != 3 || s2.StableRegions != 3 {
		t.Fatalf("frame2 (all stable): %+v", s2)
	}

	// Change sibling A only. A changes; the outer folds A so it changes; B is
	// untouched and stays stable.
	changed := append([]Surface(nil), base...)
	changed[2] = fillS(0.9)
	s3 := runFrame(&rc, changed)
	if s3.StableRegions != 1 {
		t.Fatalf("expected only sibling B stable, got %+v regions=%d", s3, s3.Regions)
	}
	if s3.Covered != 3 { // B's span: pushB, fillB, popB
		t.Fatalf("expected covered=3 (sibling B span), got %d", s3.Covered)
	}
}
