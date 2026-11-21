package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
)

type sampleSpec struct {
	name      string
	ext       string
	width     int
	height    int
	fileBytes int64
	makeStr   string
	modelStr  string
	dateStr   string
	hasGPS    bool
	lat       float64
	lon       float64
	scene     int
}

var demoSampleSpecs = []sampleSpec{
	{
		name:      "IMG_4821_big_sur_sunset.jpg",
		ext:       ".jpg",
		width:     4032,
		height:    3024,
		fileBytes: 6480210,
		makeStr:   "Apple",
		modelStr:  "iPhone 16 Pro",
		dateStr:   "2026:05:14 19:24:08",
		hasGPS:    true,
		lat:       36.2704,
		lon:       -121.8081,
		scene:     0,
	},
	{
		name:      "DSC09104_alpine_ridge.jpg",
		ext:       ".jpg",
		width:     6000,
		height:    4000,
		fileBytes: 11842600,
		makeStr:   "Sony",
		modelStr:  "ILCE-7M4",
		dateStr:   "2026:05:11 07:18:44",
		hasGPS:    true,
		lat:       46.0207,
		lon:       7.7491,
		scene:     1,
	},
	{
		name:      "IMG_4839_kyoto_twilight.jpg",
		ext:       ".jpg",
		width:     4032,
		height:    3024,
		fileBytes: 5914320,
		makeStr:   "FUJIFILM",
		modelStr:  "X100VI",
		dateStr:   "2026:04:28 18:05:19",
		hasGPS:    true,
		lat:       35.0116,
		lon:       135.7681,
		scene:     2,
	},
	{
		name:      "P1040288_harbor_night.jpg",
		ext:       ".jpg",
		width:     5472,
		height:    3648,
		fileBytes: 8740190,
		makeStr:   "Leica",
		modelStr:  "Q3",
		dateStr:   "2026:05:02 21:41:33",
		hasGPS:    true,
		lat:       35.4437,
		lon:       139.6380,
		scene:     3,
	},
	{
		name:      "IMG_4852_atacama_dunes.jpg",
		ext:       ".jpg",
		width:     4000,
		height:    3000,
		fileBytes: 4862500,
		makeStr:   "Ricoh",
		modelStr:  "GR IIIx",
		dateStr:   "2026:04:19 17:12:05",
		hasGPS:    true,
		lat:       -23.8634,
		lon:       -69.1328,
		scene:     4,
	},
	{
		name:      "DSC09118_yakushima_mist.jpg",
		ext:       ".jpg",
		width:     6240,
		height:    4160,
		fileBytes: 12410880,
		makeStr:   "FUJIFILM",
		modelStr:  "X-T5",
		dateStr:   "2026:04:25 06:51:22",
		hasGPS:    true,
		lat:       30.3586,
		lon:       130.5285,
		scene:     5,
	},
	{
		name:      "studio_botanical_macro.png",
		ext:       ".png",
		width:     2800,
		height:    2100,
		fileBytes: 7248900,
		makeStr:   "",
		modelStr:  "",
		dateStr:   "2026:05:09 14:20:00",
		hasGPS:    false,
		scene:     6,
	},
	{
		name:      "IMG_4874_lake_dawn.jpg",
		ext:       ".jpg",
		width:     4032,
		height:    3024,
		fileBytes: 5130400,
		makeStr:   "Sony",
		modelStr:  "ILCE-7M4",
		dateStr:   "2026:05:13 05:48:11",
		hasGPS:    false,
		scene:     7,
	},
}

// buildDemoPhotos constructs deterministic sample photos with genuine parsed EXIF/GPS
// payloads and painted photographic previews, without touching the filesystem.
func buildDemoPhotos() []*PhotoItem {
	items := make([]*PhotoItem, 0, len(demoSampleSpecs))
	for i, s := range demoSampleSpecs {
		preview := renderSampleScene(s.scene, 480, 320)
		thumb := scaleRGBA(preview, 112, 76)

		var meta PhotoMeta
		if s.makeStr != "" || s.modelStr != "" || s.hasGPS {
			// Encode a small JPEG and inject real EXIF so our real parser inspects it.
			var jb bytes.Buffer
			_ = jpeg.Encode(&jb, thumb, &jpeg.Options{Quality: 78})
			app1 := BuildCleanEXIF(s.makeStr, s.modelStr, s.dateStr, s.hasGPS, s.lat, s.lon)
			withExif := InjectJPEGAPP1(jb.Bytes(), app1)
			meta = InspectJPEGEXIF(withExif)
		}

		items = append(items, &PhotoItem{
			ID:        i + 1,
			Name:      s.name,
			Path:      "/Users/alex/Pictures/Spring-Trip-2026/" + s.name,
			Ext:       s.ext,
			Width:     s.width,
			Height:    s.height,
			FileBytes: s.fileBytes,
			Meta:      meta,
			Selected:  true,
			Thumb:     thumb,
			Preview:   preview,
			ThumbKey:  "demo-thumb-" + s.name,
			PrevKey:   "demo-prev-" + s.name,
			DemoScene: s.scene,
			IsDemo:    true,
		})
	}
	return items
}

func lerp(a, b, t float64) float64 {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return a + (b-a)*t
}

type rgb struct{ r, g, b float64 }

func lerpRGB(a, b rgb, t float64) rgb {
	return rgb{lerp(a.r, b.r, t), lerp(a.g, b.g, t), lerp(a.b, b.b, t)}
}

func addRGB(a, b rgb, k float64) rgb {
	return rgb{a.r + b.r*k, a.g + b.g*k, a.b + b.b*k}
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func smoothstep(e0, e1, x float64) float64 {
	t := (x - e0) / (e1 - e0)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

// fbm1 produces deterministic smooth mountain/cloud silhouettes.
func fbm1(x, seed float64) float64 {
	v := 0.50 * math.Sin(x*2.1+seed)
	v += 0.28 * math.Sin(x*4.7+seed*1.9+1.3)
	v += 0.14 * math.Sin(x*9.3+seed*3.1+0.6)
	v += 0.08 * math.Sin(x*19.1+seed*5.2+2.4)
	return v
}

// renderSampleScene paints a deterministic photographic landscape/studio image.
func renderSampleScene(scene, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fw, fh := float64(w), float64(h)

	for y := 0; y < h; y++ {
		ny := float64(y) / fh
		for x := 0; x < w; x++ {
			nx := float64(x) / fw
			var c rgb

			switch scene {
			case 0: // Big Sur golden hour coastal sunset
				// Warm glowing sky -> coral horizon
				skyTop := rgb{34, 48, 88}
				skyMid := rgb{142, 74, 96}
				skyLow := rgb{248, 166, 94}
				if ny < 0.56 {
					t := ny / 0.56
					if t < 0.52 {
						c = lerpRGB(skyTop, skyMid, t/0.52)
					} else {
						c = lerpRGB(skyMid, skyLow, (t-0.52)/0.48)
					}
					// Soft glowing sun disc near horizon
					dx, dy := (nx-0.62)*1.5, ny-0.46
					d := math.Hypot(dx, dy)
					glow := math.Exp(-d * 4.8)
					c = addRGB(c, rgb{255, 224, 150}, glow*0.72)
					if d < 0.065 {
						c = lerpRGB(c, rgb{255, 248, 224}, smoothstep(0.065, 0.03, d))
					}
					// Streaked dusk clouds
					cloud := math.Sin(nx*9.0+ny*14.0+0.6)*0.5 + 0.5
					cloudBand := math.Exp(-math.Pow((ny-0.28)/0.11, 2))
					c = lerpRGB(c, rgb{246, 148, 108}, cloud*cloudBand*0.32)
				} else {
					// Ocean water with shimmering sun path
					wt := (ny - 0.56) / 0.44
					c = lerpRGB(rgb{106, 78, 96}, rgb{22, 34, 54}, math.Pow(wt, 0.72))
					sunCol := math.Exp(-math.Pow((nx-0.62)/(0.05+wt*0.14), 2))
					ripple := 0.55 + 0.45*math.Sin(ny*160.0+nx*18.0)
					c = addRGB(c, rgb{255, 198, 118}, sunCol*ripple*(1.0-wt*0.35)*0.85)
				}
				// Distant headland silhouette
				farRidge := 0.55 - 0.07*math.Max(0, 1.0-nx*1.35) + 0.018*fbm1(nx*4.2, 1.1)
				if ny > farRidge && ny < 0.62 && nx < 0.72 {
					c = lerpRGB(c, rgb{62, 50, 68}, 0.85)
				}
				// Foreground dramatic coastal cliff on left
				cliff := 0.48 + 0.34*smoothstep(0.18, 0.52, nx) + 0.03*fbm1(nx*6.0, 2.7)
				if ny > cliff {
					c = lerpRGB(rgb{26, 22, 28}, rgb{46, 36, 34}, smoothstep(cliff, cliff+0.15, ny))
				}

			case 1: // Snowy alpine mountain ridge in crisp morning light
				skyTop := rgb{46, 98, 166}
				skyBot := rgb{186, 216, 238}
				c = lerpRGB(skyTop, skyBot, math.Pow(ny, 0.75))
				// High sunlit clouds
				ch := math.Exp(-math.Pow((ny-0.18)/0.08, 2)) * (0.5 + 0.5*fbm1(nx*3.8, 0.5))
				c = lerpRGB(c, rgb{250, 248, 244}, ch*0.55)
				// Main Matterhorn-like jagged peak
				peak := 0.54 - 0.34*math.Exp(-math.Pow((nx-0.54)/0.22, 2)) - 0.14*math.Exp(-math.Pow((nx-0.22)/0.18, 2)) + 0.035*fbm1(nx*5.5, 4.2)
				if ny > peak {
					// Sunlit east face vs cool shadowed west face
					slope := fbm1(nx*8.0+0.2, 4.2) - fbm1(nx*8.0-0.2, 4.2)
					snow := rgb{242, 245, 250}
					rock := rgb{72, 86, 108}
					shade := smoothstep(-0.22, 0.24, slope+(nx-0.54)*0.8)
					c = lerpRGB(rock, snow, shade*0.85+0.12)
				}
				// Mid-ground pine valley ridge
				mid := 0.68 + 0.05*fbm1(nx*3.5, 8.1) - 0.04*(1.0-nx)
				if ny > mid {
					t := smoothstep(mid, 1.0, ny)
					c = lerpRGB(rgb{44, 68, 76}, rgb{26, 44, 48}, t)
				}

			case 2: // Kyoto autumn twilight & vermilion silhouette
				c = lerpRGB(rgb{40, 44, 78}, rgb{222, 126, 92}, math.Pow(ny, 0.9))
				// Soft twilight glow
				g := math.Exp(-math.Hypot((nx-0.5)*1.2, ny-0.52) * 3.2)
				c = addRGB(c, rgb{255, 178, 112}, g*0.55)
				// Distant hills
				hill := 0.60 + 0.05*fbm1(nx*2.8, 5.0)
				if ny > hill {
					c = lerpRGB(rgb{52, 42, 58}, rgb{28, 22, 30}, smoothstep(hill, 1.0, ny))
				}
				// Classic vermilion torii gate silhouette in mid-ground
				inPillarL := math.Abs(nx-0.41) < 0.017 && ny > 0.42 && ny < 0.84
				inPillarR := math.Abs(nx-0.59) < 0.017 && ny > 0.42 && ny < 0.84
				archCurve := 0.41 - 0.018*math.Pow((nx-0.5)/0.16, 2)
				inTopBeam := math.Abs(nx-0.5) < 0.16 && ny > archCurve && ny < archCurve+0.028
				inSubBeam := math.Abs(nx-0.5) < 0.13 && ny > 0.475 && ny < 0.497
				if inPillarL || inPillarR || inTopBeam || inSubBeam {
					c = lerpRGB(rgb{182, 42, 30}, rgb{124, 24, 22}, smoothstep(0.4, 0.85, ny))
				}
				// Autumn maple foliage framing top-left and top-right
				if ny < 0.42 && (nx < 0.28 || nx > 0.74) {
					d := math.Min(nx/0.28, (1.0-nx)/0.26)
					leaf := 0.5 + 0.5*math.Sin(nx*48.0)*math.Cos(ny*42.0)
					if d+ny*0.7+leaf*0.25 < 0.72 {
						c = lerpRGB(rgb{158, 36, 26}, rgb{216, 94, 34}, leaf)
					}
				}

			case 3: // Night harbor skyline with warm bokeh & water reflections
				if ny < 0.58 {
					c = lerpRGB(rgb{10, 16, 32}, rgb{26, 38, 66}, ny/0.58)
				} else {
					wt := (ny - 0.58) / 0.42
					c = lerpRGB(rgb{24, 34, 56}, rgb{8, 12, 24}, wt)
				}
				// City skyline silhouettes
				bIdx := int(nx * 24)
				bh := 0.58 - 0.05 - 0.16*math.Abs(math.Sin(float64(bIdx)*1.73+0.4))
				if ny > bh && ny < 0.58 {
					c = rgb{16, 22, 36}
					// Lit office windows
					wx := math.Mod(nx*24, 1.0)
					wy := math.Mod(ny*36, 1.0)
					if wx > 0.22 && wx < 0.78 && wy > 0.25 && wy < 0.68 {
						hash := math.Sin(float64(bIdx)*12.9898+math.Floor(ny*36)*78.233) * 43758.5453
						if hash-math.Floor(hash) > 0.48 {
							c = rgb{250, 210, 126}
						}
					}
				}
				// Warm & cyan water reflections below horizon
				if ny >= 0.58 {
					wt := (ny - 0.58) / 0.42
					shim := 0.5 + 0.5*math.Sin(ny*180.0+float64(bIdx)*2.1)
					glowGold := math.Exp(-math.Pow((nx-0.34)/0.07, 2)) + math.Exp(-math.Pow((nx-0.68)/0.06, 2))
					glowCyan := math.Exp(-math.Pow((nx-0.52)/0.08, 2))
					c = addRGB(c, rgb{255, 192, 92}, glowGold*shim*(1-wt*0.55)*0.65)
					c = addRGB(c, rgb{88, 214, 240}, glowCyan*shim*(1-wt*0.5)*0.45)
				}

			case 4: // Atacama golden desert dunes
				c = lerpRGB(rgb{68, 114, 172}, rgb{238, 196, 152}, math.Pow(ny/0.5, 0.85))
				if ny > 0.46 {
					c = rgb{222, 168, 112}
				}
				// Three layered sweeping sand dunes with crisp sunlit/shadowed crests
				d1 := 0.48 + 0.06*math.Sin(nx*4.2+0.5)
				d2 := 0.60 + 0.08*math.Sin(nx*3.6+2.2)
				d3 := 0.74 + 0.09*math.Sin(nx*2.9+4.4)
				if ny > d1 {
					c = lerpRGB(rgb{232, 174, 108}, rgb{172, 106, 68}, smoothstep(d1, d1+0.12, ny))
				}
				if ny > d2 {
					side := smoothstep(0.32, 0.72, nx)
					c = lerpRGB(rgb{240, 180, 106}, rgb{148, 86, 56}, side*0.85)
				}
				if ny > d3 {
					side := smoothstep(0.18, 0.58, nx)
					c = lerpRGB(rgb{118, 66, 46}, rgb{218, 148, 84}, side)
				}

			case 5: // Yakushima emerald misty forest ridges
				c = lerpRGB(rgb{182, 206, 200}, rgb{226, 236, 230}, math.Pow(ny, 0.85))
				// Soft morning sun glow breaking through upper-left mist
				beam := math.Exp(-math.Hypot((nx-0.28)*1.3, ny-0.22) * 3.0)
				c = addRGB(c, rgb{255, 248, 224}, beam*0.42)
				// Four layered atmospheric forested mountain ridges with pine canopy silhouettes
				ridges := []struct {
					y0   float64
					amp  float64
					seed float64
					col  rgb
					mist float64
				}{
					{0.40, 0.07, 1.4, rgb{114, 146, 134}, 0.45},
					{0.52, 0.08, 3.7, rgb{72, 110, 94}, 0.30},
					{0.66, 0.07, 6.2, rgb{42, 76, 62}, 0.16},
					{0.79, 0.05, 9.5, rgb{22, 46, 36}, 0.05},
				}
				for idx, r := range ridges {
					treeJagged := 0.014 * math.Abs(math.Sin(nx*(34.0+float64(idx)*12.0)+r.seed))
					yLine := r.y0 + r.amp*fbm1(nx*(2.6+float64(idx)*0.8), r.seed) - treeJagged
					if ny > yLine {
						depth := smoothstep(yLine, yLine+0.14, ny)
						mistLayer := lerpRGB(r.col, rgb{196, 218, 210}, r.mist*(1.0-depth*0.6))
						c = lerpRGB(c, mistLayer, 0.92)
					}
				}

			case 6: // Botanical studio macro with soft bokeh circles
				c = lerpRGB(rgb{34, 58, 46}, rgb{18, 30, 26}, math.Hypot(nx-0.5, ny-0.5)*1.2)
				// Soft bokeh orbs
				orbs := [][4]float64{
					{0.20, 0.26, 0.14, 0.35},
					{0.78, 0.22, 0.18, 0.32},
					{0.82, 0.72, 0.15, 0.28},
					{0.26, 0.76, 0.12, 0.24},
					{0.52, 0.32, 0.10, 0.25},
				}
				for _, o := range orbs {
					d := math.Hypot((nx-o[0])*1.4, ny-o[1])
					if d < o[2] {
						soft := smoothstep(o[2], o[2]*0.65, d)
						c = addRGB(c, rgb{184, 232, 168}, soft*o[3])
					}
				}
				// Sharp foreground botanical leaf curving across center
				leafCenter := 0.56 - 0.18*math.Sin((nx-0.18)*3.8)
				leafW := 0.18 * math.Sin(math.Max(0, math.Min(1, (nx-0.16)/0.68))*math.Pi)
				if nx > 0.16 && nx < 0.84 && math.Abs(ny-leafCenter) < leafW {
					dist := math.Abs(ny-leafCenter) / (leafW + 1e-6)
					vein := math.Exp(-dist * 14.0)
					leafCol := lerpRGB(rgb{76, 152, 88}, rgb{36, 92, 52}, dist)
					leafCol = addRGB(leafCol, rgb{176, 224, 138}, vein*0.48)
					c = leafCol
				}

			default: // Tranquil alpine lake at dawn
				if ny < 0.54 {
					c = lerpRGB(rgb{72, 92, 136}, rgb{246, 198, 174}, math.Pow(ny/0.54, 0.9))
				} else {
					wt := (ny - 0.54) / 0.46
					c = lerpRGB(rgb{224, 186, 172}, rgb{66, 86, 118}, math.Pow(wt, 0.8))
				}
				ridge := 0.54 - 0.13*math.Exp(-math.Pow((nx-0.32)/0.25, 2)) - 0.10*math.Exp(-math.Pow((nx-0.78)/0.22, 2)) + 0.02*fbm1(nx*4.8, 9.2)
				if ny > ridge && ny < 0.54 {
					c = lerpRGB(rgb{82, 94, 122}, rgb{52, 62, 86}, smoothstep(ridge, 0.54, ny))
				}
				// Mirror reflection of mountain in calm water
				if ny >= 0.54 {
					mirrorY := 0.54 - (ny - 0.54)
					if mirrorY > ridge {
						ripple := 0.88 + 0.12*math.Sin(ny*140.0+nx*12.0)
						c = lerpRGB(c, rgb{62, 74, 98}, 0.55*ripple)
					}
				}
			}

			// Subtle photographic vignette for natural lens depth
			vig := 1.0 - 0.18*math.Pow(math.Hypot(nx-0.5, ny-0.5)*1.35, 2)
			img.SetRGBA(x, y, color.RGBA{
				R: clampByte(c.r * vig),
				G: clampByte(c.g * vig),
				B: clampByte(c.b * vig),
				A: 255,
			})
		}
	}
	return img
}
