package main

import (
	"image"
	"math"

	. "go.hasen.dev/shirei"
)

type ringCacheKey struct {
	step   int
	cat    Category
	status TimerStatus
	flash  int
	devPx  int
}

var (
	cachedRingKey ringCacheKey
	cachedRingImg *image.RGBA
)

// getRingImage returns a cached premultiplied RGBA image of the radial steeping
// ring at device resolution (logicalSize * scale) for a 1:1 HiDPI blit.
func getRingImage(logicalSize float32, frac float32, cat Category, status TimerStatus, flash float32) ImageId {
	scale := GetHost().WindowScale
	if scale < 1 {
		scale = 1
	}
	devPx := int(math.Round(float64(logicalSize * scale)))
	if devPx < 32 {
		devPx = 32
	}

	// Quantize to ~360 angular steps so static/slow frames reuse the exact buffer.
	step := int(math.Round(float64(frac) * 360))
	if step < 0 {
		step = 0
	}
	if step > 360 {
		step = 360
	}
	flashStep := int(math.Round(float64(flash) * 20))

	key := ringCacheKey{
		step:   step,
		cat:    cat,
		status: status,
		flash:  flashStep,
		devPx:  devPx,
	}
	if cachedRingImg == nil || cachedRingKey != key {
		cachedRingKey = key
		cachedRingImg = renderRingRGBA(devPx, float64(step)/360.0, cat, status, float64(flashStep)/20.0)
	}
	return UseImage("steep-hud-ring", cachedRingImg)
}

func smoothstep(edge0, edge1, x float64) float64 {
	if edge1 == edge0 {
		if x < edge0 {
			return 0
		}
		return 1
	}
	t := (x - edge0) / (edge1 - edge0)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

func hslaToRGB(c Vec4) (r, g, b float64) {
	rf, gf, bf := FloatHSLToRGB(c[0]/360, c[1]/100, c[2]/100)
	return float64(rf), float64(gf), float64(bf)
}

// blendOverPremul composites straight-alpha (sr, sg, sb, sa) over premultiplied (dr, dg, db, da).
func blendOverPremul(dr, dg, db, da, sr, sg, sb, sa float64) (float64, float64, float64, float64) {
	if sa <= 0 {
		return dr, dg, db, da
	}
	inv := 1 - sa
	return sr*sa + dr*inv, sg*sa + dg*inv, sb*sa + db*inv, sa + da*inv
}

// renderRingRGBA draws an anti-aliased radial gauge with subtle cardinal ticks,
// a smooth circular track, rounded arc caps, and a luminous leading tip.
func renderRingRGBA(px int, frac float64, cat Category, status TimerStatus, flash float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, px, px))

	cx := float64(px) * 0.5
	cy := float64(px) * 0.5
	radius := float64(px) * 0.405
	stroke := float64(px) * 0.072
	halfStroke := stroke * 0.5
	aa := 1.15 // ~1px feather for crisp anti-aliasing

	accent := cat.AccentHSLA()
	if status == StatusPaused {
		accent[1] *= 0.55
		accent[2] *= 0.90
	}
	if status == StatusDone {
		accent[2] = min(74, accent[2]+8)
	}
	ar, ag, ab := hslaToRGB(accent)

	// Lighter specular tint for the leading head of the arc.
	brightAccent := Vec4{accent[0], min(95, accent[1]+10), min(88, accent[2]+20), 1}
	br, bg, bb := hslaToRGB(brightAccent)

	// Subtle warm inner halo when done or chiming.
	haloStrength := 0.0
	if status == StatusDone {
		haloStrength = 0.22 + 0.14*flash
	} else if flash > 0 {
		haloStrength = 0.18 * flash
	}

	sweep := frac * 2 * math.Pi
	startX, startY := cx, cy-radius
	endAngle := -math.Pi*0.5 + sweep
	endX := cx + radius*math.Cos(endAngle)
	endY := cy + radius*math.Sin(endAngle)

	for y := 0; y < px; y++ {
		py := float64(y) + 0.5
		dy := py - cy
		for x := 0; x < px; x++ {
			pxf := float64(x) + 0.5
			dx := pxf - cx
			dist := math.Hypot(dx, dy)

			var pr, pg, pb, pa float64

			// 1. Optional gentle radial inner glow when done/chiming.
			if haloStrength > 0 && dist < radius+halfStroke*1.8 {
				t := 1 - dist/(radius+halfStroke*1.8)
				if t > 0 {
					pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, ar, ag, ab, haloStrength*t*t)
				}
			}

			// 2. Subtle inner disc tint behind the icon for depth.
			innerDiscR := radius - halfStroke*1.55
			if dist < innerDiscR+aa {
				cov := 1 - smoothstep(innerDiscR-aa, innerDiscR+aa, dist)
				pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, 1, 1, 1, 0.045*cov)
			}

			// 3. Circular background track.
			trackDist := math.Abs(dist - radius)
			if trackDist < halfStroke+aa {
				cov := 1 - smoothstep(halfStroke-aa, halfStroke+aa, trackDist)
				pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, 0.88, 0.90, 0.94, 0.14*cov)
			}

			// 4. Tiny cardinal tick marks inside the track (12, 3, 6, 9 o'clock).
			tickR := radius - halfStroke - float64(px)*0.036
			tickDot := float64(px) * 0.015
			for k := 0; k < 4; k++ {
				ang := float64(k) * math.Pi * 0.5
				tx := cx + tickR*math.Cos(ang)
				ty := cy + tickR*math.Sin(ang)
				td := math.Hypot(pxf-tx, py-ty)
				if td < tickDot+aa {
					cov := 1 - smoothstep(tickDot-aa, tickDot+aa, td)
					pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, 0.92, 0.93, 0.96, 0.24*cov)
				}
			}

			// 5. Active progress sweep arc (clockwise from 12 o'clock) with rounded caps.
			if frac > 0.002 {
				// Angle clockwise from 12 o'clock in [0, 2*pi).
				ang := math.Atan2(dy, dx) + math.Pi*0.5
				if ang < 0 {
					ang += 2 * math.Pi
				}

				var arcCov float64
				var dStart, dEnd float64
				if frac >= 0.998 {
					arcCov = 1 - smoothstep(halfStroke-aa, halfStroke+aa, trackDist)
				} else {
					if ang <= sweep {
						arcCov = 1 - smoothstep(halfStroke-aa, halfStroke+aa, trackDist)
					}
					// Rounded start cap (12 o'clock) and end cap (leading edge).
					dStart = math.Hypot(pxf-startX, py-startY)
					capStart := 1 - smoothstep(halfStroke-aa, halfStroke+aa, dStart)
					if capStart > arcCov {
						arcCov = capStart
					}
					dEnd = math.Hypot(pxf-endX, py-endY)
					capEnd := 1 - smoothstep(halfStroke-aa, halfStroke+aa, dEnd)
					if capEnd > arcCov {
						arcCov = capEnd
					}
				}

				if arcCov > 0 {
					// Subtle gradient from rich base accent to bright leading tip,
					// keeping caps and full-circle done rings completely seamless.
					t := 0.35
					if frac < 0.998 && sweep > 0.001 {
						if ang <= sweep {
							t = ang / sweep
						} else if dStart < dEnd {
							t = 0
						} else {
							t = 1
						}
					}
					cr := ar + (br-ar)*(0.45*t)
					cg := ag + (bg-ag)*(0.45*t)
					cb := ab + (bb-ab)*(0.45*t)
					pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, cr, cg, cb, 0.96*arcCov)
				}

				// Soft luminous dot right on the leading edge while actively counting.
				if frac < 0.998 && status != StatusIdle {
					dEnd := math.Hypot(pxf-endX, py-endY)
					glowR := halfStroke * 2.0
					if dEnd < glowR {
						gt := 1 - dEnd/glowR
						pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, br, bg, bb, 0.34*gt*gt)
					}
					dotR := halfStroke * 0.56
					if dEnd < dotR+aa {
						dcov := 1 - smoothstep(dotR-aa, dotR+aa, dEnd)
						pr, pg, pb, pa = blendOverPremul(pr, pg, pb, pa, 1, 1, 1, 0.85*dcov)
					}
				}
			}

			off := img.PixOffset(x, y)
			img.Pix[off+0] = clampByte(pr * 255)
			img.Pix[off+1] = clampByte(pg * 255)
			img.Pix[off+2] = clampByte(pb * 255)
			img.Pix[off+3] = clampByte(pa * 255)
		}
	}

	return img
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
