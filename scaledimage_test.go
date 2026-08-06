package shirei

import (
	"testing"
	"time"
)

func TestQuantizeDim(t *testing.T) {
	cases := []struct {
		v, step, want int
	}{
		{0, 8, 0},
		{1, 8, 1},
		{7, 8, 8},
		{8, 8, 8},
		{11, 8, 8},
		{12, 8, 16},
		{100, 0, 100},
		{100, 1, 100},
	}
	for _, c := range cases {
		if got := quantizeDim(c.v, c.step); got != c.want {
			t.Errorf("quantizeDim(%d, %d) = %d, want %d", c.v, c.step, got, c.want)
		}
	}
}

func TestNoteScaleMotion(t *testing.T) {
	savedIdle := ScaleMotionIdle
	ScaleMotionIdle = 50 * time.Millisecond
	defer func() { ScaleMotionIdle = savedIdle }()

	res.scaleMotionById = map[ImageId]scaleMotion{}
	const id ImageId = 42

	if noteScaleMotion(id, 100, 80) {
		t.Fatal("first sighting should not be motion")
	}
	if noteScaleMotion(id, 100, 80) {
		t.Fatal("unchanged size after first sighting should not be motion")
	}
	if !noteScaleMotion(id, 120, 80) {
		t.Fatal("size change should be motion")
	}
	if !noteScaleMotion(id, 120, 80) {
		t.Fatal("still within idle window should be motion")
	}
}
