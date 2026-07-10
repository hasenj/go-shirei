package main

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Balls & Buckets", 720, 520)
	app.Run(appView)
}

// BallPayload is the DnD drag payload for a ball (also used as its container key).
type BallPayload string

// BucketTarget is the DnD drop-zone payload: "" is the unassigned tray; "A"…"J"
// are buckets (also used as those containers' keys).
type BucketTarget string

type Ball struct {
	Id     string
	Name   string
	Hue    float32
	Bucket string // "" = unassigned
}

var balls = []Ball{
	{Id: "red", Name: "Red Ball", Hue: 0},
	{Id: "blue", Name: "Blue Ball", Hue: 220},
	{Id: "green", Name: "Green Ball", Hue: 140},
}

var bucketLetters = []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}

func appView() {
	ModAttrs(Pad(16), Gap(16), Background(0, 0, 96, 1))

	Label("Balls & Buckets", FontSize(22), FontWeight(WeightBold))
	Label("Drag a ball into a bucket, between buckets, or back to the tray.", FontSize(13), TextColor(0, 0, 40, 1))

	// Unassigned tray — drop target BucketTarget("")
	ContainerWithKey(BucketTarget(""), Attrs(Pad(12), Gap(10), Corners(6), Background(0, 0, 90, 1), Expand), func() {
		if CanDropHere[BallPayload](BucketTarget("")) {
			ModAttrs(Background(0, 0, 86, 1), BorderWidth(2), BorderColor(210, 60, 50, 1))
		}
		Label("Unassigned", FontSize(14), FontWeight(WeightBold), TextColor(0, 0, 35, 1))
		Container(Attrs(Row, Wrap, Gap(10), CrossMid), func() {
			for i := range balls {
				if balls[i].Bucket == "" {
					ballCard(i)
				}
			}
		})
	})

	// Buckets
	Label("Buckets", FontSize(14), FontWeight(WeightBold), TextColor(0, 0, 35, 1))
	Container(Attrs(Row, Wrap, Gap(10), Expand), func() {
		for _, letter := range bucketLetters {
			bucketBox(letter)
		}
	})

	// Status
	Element(Attrs(Expand, MinHeight(1), Background(0, 0, 0, 0.12)))
	Label("Status", FontSize(14), FontWeight(WeightBold), TextColor(0, 0, 35, 1))
	for i := range balls {
		b := &balls[i]
		var line string
		if b.Bucket == "" {
			line = fmt.Sprintf("%s not in any Bucket", b.Name)
		} else {
			line = fmt.Sprintf("%s in Bucket %s", b.Name, b.Bucket)
		}
		Label(line, FontSize(13), TextColor(0, 0, 25, 1))
	}

	// Ghost while dragging
	if payload, ok := GetDraggingItem[BallPayload](); ok {
		idx := ballIndex(string(payload))
		if idx >= 0 {
			rect := GetDraggingItemRect()
			ContainerWithKey("dnd-ghost", ballAttrs(balls[idx]), func() {
				ModAttrs(NoAnimate, FloatVec(rect.Origin), FixSizeVec(rect.Size), ClickThrough, Trans(0.55))
				Label(balls[idx].Name, FontSize(13), TextColor(0, 0, 100, 1), FontWeight(WeightBold))
			})
		}
	}
}

func bucketBox(letter string) {
	target := BucketTarget(letter)
	ContainerWithKey(target, Attrs(Pad(10), Gap(8), Corners(6), MinWidth(120), MinHeight(100), Background(0, 0, 88, 1), BorderWidth(1), BorderColor(0, 0, 75, 1)), func() {
		if CanDropHere[BallPayload](target) {
			ModAttrs(Background(0, 0, 82, 1), BorderColor(210, 60, 50, 1), BorderWidth(2))
		}
		Label("Bucket "+letter, FontSize(13), FontWeight(WeightBold))
		Container(Attrs(Row, Wrap, Gap(6)), func() {
			for i := range balls {
				if balls[i].Bucket == letter {
					ballCard(i)
				}
			}
		})
	})
}

func ballAttrs(b Ball) AttrSet {
	return Attrs(Pad2(8, 14), Gap(4), Corners(20), Background(b.Hue, 70, 45, 1), BorderWidth(1), BorderColor(b.Hue, 70, 30, 1))
}

func ballCard(i int) {
	b := &balls[i]
	payload := BallPayload(b.Id)
	ContainerWithKey(payload, ballAttrs(*b), func() {
		if IsHovered() {
			ModAttrs(Background(b.Hue, 70, 52, 1))
		}
		if IsDragging() {
			ModAttrs(Background(b.Hue, 40, 70, 1), Trans(0.35))
		}
		if DragAndDrop(payload) {
			b.Bucket = string(GetDropTarget[BucketTarget]())
		}
		Label(b.Name, FontSize(13), TextColor(0, 0, 100, 1), FontWeight(WeightBold))
	})
}

func ballIndex(id string) int {
	for i := range balls {
		if balls[i].Id == id {
			return i
		}
	}
	return -1
}
