package shirei

import (
	"image"
	"testing"
)

func fillRGBA(v byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = v
	}
	return img
}

func TestPutImageReusesIdForSameKey(t *testing.T) {
	key := "test-put-reuse"
	id1 := UseImage(key, fillRGBA(0x11))
	id2 := UseImage(key, fillRGBA(0x22))
	if id1 == 0 || id2 == 0 {
		t.Fatalf("expected non-zero ids, got %d and %d", id1, id2)
	}
	if id1 != id2 {
		t.Fatalf("same key should reuse id: %d vs %d", id1, id2)
	}
	data := LookupImage(id1)
	if data == nil || data.Pix[0] != 0x22 {
		t.Fatalf("replace should update pixels behind the id")
	}
}

func TestUseImageSameBackingKeepsGeneration(t *testing.T) {
	const key = "test-useimage-same-backing"
	rgba := fillRGBA(0xAA)
	id1 := UseImage(key, rgba)
	g1 := LookupImage(id1).Generation
	id2 := UseImage(key, rgba)
	g2 := LookupImage(id2).Generation
	if id1 != id2 {
		t.Fatalf("same key+backing should reuse id: %d vs %d", id1, id2)
	}
	if g1 != g2 {
		t.Fatalf("re-UseImage with same backing must not bump Generation: %d → %d", g1, g2)
	}
	// Third call: still stable (browse-demo every-frame pattern).
	_ = UseImage(key, rgba)
	if g := LookupImage(id1).Generation; g != g1 {
		t.Fatalf("Generation drifted on third UseImage: %d want %d", g, g1)
	}
}

func TestUseImageNewBufferBumpsGeneration(t *testing.T) {
	const key = "test-useimage-new-buffer"
	id := UseImage(key, fillRGBA(0x11))
	g1 := LookupImage(id).Generation
	id2 := UseImage(key, fillRGBA(0x22)) // new allocation → different Pix
	g2 := LookupImage(id2).Generation
	if id != id2 {
		t.Fatalf("same key should reuse id: %d vs %d", id, id2)
	}
	if g2 == g1 {
		t.Fatal("new buffer under same key must bump Generation")
	}
	if LookupImage(id).Pix[0] != 0x22 {
		t.Fatal("replace should install new pixels")
	}
}

func TestPutImageDistinctKeysGetDistinctIds(t *testing.T) {
	a := UseImage("test-put-a", fillRGBA(1))
	b := UseImage("test-put-b", fillRGBA(2))
	if a == 0 || b == 0 || a == b {
		t.Fatalf("distinct keys need distinct non-zero ids: %d %d", a, b)
	}
}

func TestShadowGoesThroughSharedRegistry(t *testing.T) {
	// Same params → same id (getOrPut hit), and the id lives in res.imageKeys
	// under ShadowMapKey, not a private map.
	size := Vec2{20, 10}
	corners := Vec4{2, 2, 2, 2}
	const radius, alpha = float32(1.5), float32(0.4)

	id1 := _IMBlurShadow(size, corners, radius, alpha)
	id2 := _IMBlurShadow(size, corners, radius, alpha)
	if id1 == 0 {
		t.Fatal("shadow id should be non-zero")
	}
	if id1 != id2 {
		t.Fatalf("same shadow params should reuse id: %d vs %d", id1, id2)
	}
	if LookupImage(id1) == nil {
		t.Fatal("shadow id must resolve via LookupImage / res.imageIds")
	}

	params := ShadowMapKey{
		w:  int(size[0]),
		h:  int(size[1]),
		c0: uint8(corners[0]),
		c1: uint8(corners[1]),
		c2: uint8(corners[2]),
		c3: uint8(corners[3]),
		r:  uint8(radius * 10),
		a:  uint8(alpha * 0xff),
	}
	if got := imageIdForKey(params); got != id1 {
		t.Fatalf("shadow must be registered under ShadowMapKey in res.imageKeys: got %d want %d", got, id1)
	}

	// Different params → different id
	id3 := _IMBlurShadow(Vec2{21, 10}, corners, radius, alpha)
	if id3 == 0 || id3 == id1 {
		t.Fatalf("different shadow params should get a new id: %d (was %d)", id3, id1)
	}
}

func TestImageEvictionFreesAndReloads(t *testing.T) {
	savedN := contentCachePruneAfterFrames
	contentCachePruneAfterFrames = 2
	defer func() { contentCachePruneAfterFrames = savedN }()

	const key = "test-evict-reload"
	ui.FrameNumber = 1000
	id1 := UseImage(key, fillRGBA(0x11))
	if id1 == 0 {
		t.Fatal("expected id")
	}
	if LookupImage(id1) == nil {
		t.Fatal("live after UseImage")
	}

	// Untouched for prune window → free.
	ui.FrameNumber = 1003 // lastUsed=1000, stale=1001 → free
	maybeSweepImages()
	if LookupImage(id1) != nil {
		t.Fatalf("id %d should be freed", id1)
	}
	if imageIdForKey(key) != 0 {
		t.Fatal("key should be gone from res.imageKeys")
	}
	st := DebugGetImageCacheStats()
	if st.FreeList < 1 {
		t.Fatalf("free list empty after free: %+v", st)
	}

	// Re-register: free-list should reuse the id; pixels come from new rgba.
	ui.FrameNumber = 1004
	id2 := UseImage(key, fillRGBA(0x22))
	if id2 != id1 {
		t.Fatalf("expected free-list reuse of id %d, got %d", id1, id2)
	}
	data := LookupImage(id2)
	if data == nil || data.Pix[0] != 0x22 {
		t.Fatal("reload should install new pixels")
	}
}

func TestImageEvictionSkipsTouched(t *testing.T) {
	savedN := contentCachePruneAfterFrames
	contentCachePruneAfterFrames = 2
	defer func() { contentCachePruneAfterFrames = savedN }()

	ui.FrameNumber = 2000
	id := UseImage("test-evict-touch", fillRGBA(0x33))
	ui.FrameNumber = 2002
	touchImage(id) // keep alive
	ui.FrameNumber = 2003
	maybeSweepImages()
	if LookupImage(id) == nil {
		t.Fatal("touched image should not be freed")
	}
}

func TestShadowKeyDoesNotCollideWithStringKey(t *testing.T) {
	// map[any] keeps string and ShadowMapKey in separate key spaces even if
	// a string looked "similar" — smoke-check that a string put doesn't
	// overwrite a shadow slot.
	params := ShadowMapKey{w: 8, h: 8, r: 5, a: 100}
	sid := getOrPutImage(params, func() *ImageData {
		return &ImageData{Config: image.Config{Width: 1, Height: 1}}
	})
	// App key that is unrelated; must not equal sid's lookup via string
	aid := UseImage("unrelated-app-key", fillRGBA(9))
	if sid == 0 || aid == 0 {
		t.Fatal("expected ids")
	}
	if imageIdForKey(params) != sid {
		t.Fatal("string registration must not disturb ShadowMapKey entry")
	}
	if imageIdForKey("unrelated-app-key") != aid {
		t.Fatal("shadow registration must not disturb string key entry")
	}
}
