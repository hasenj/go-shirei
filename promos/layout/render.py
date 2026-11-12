"""Render the 24-second Shirei layout promo from engine geometry and frames.

Requires Pillow and ffmpeg. --preview produces a storyboard without encoding.
"""

import argparse
import functools
import json
import subprocess
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = Path(__file__).resolve().parent
W, H, FPS, DURATION = 1920, 1080, 30, 24
PAPER, INK, RED = "#F6F2E9", "#211C19", "#8A1F1F"
DARK, WHITE, ACCENT = "#1C2635", "#F6F2E9", "#E5B281"
MUTED = "#AFBAC9"
COLORS = ("#425E86", "#B77766", "#AD8039")


def ease(value):
    value = max(0.0, min(1.0, value))
    return 1 - (1 - value) ** 3


def enter(t, delay=0, duration=.8):
    return ease((t - delay) / duration)


@functools.lru_cache(maxsize=256)
def font(size, weight="regular"):
    path = args.mono if weight == "mono" else args.font
    index = {"regular": 7, "medium": 5, "bold": 0, "heavy": 8, "mono": 0}[weight]
    if Path(path).name != "Avenir Next.ttc":
        index = 0
    return ImageFont.truetype(path, size, index=index)


@functools.lru_cache(maxsize=512)
def letters(value, size, color, weight):
    face = font(size, weight)
    box = face.getbbox(value)
    tile = Image.new("RGBA", (max(1, box[2] - box[0] + 6), box[3] - box[1] + 6))
    ImageDraw.Draw(tile).text((3 - box[0], 3 - box[1]), value, font=face, fill=color)
    return tile


def paste(dst, src, x, y, opacity=1):
    if opacity <= 0:
        return
    if opacity < .999:
        src = src.copy()
        src.putalpha(src.getchannel("A").point(lambda p: round(p * opacity)))
    dst.alpha_composite(src, (round(x), round(y)))


def text(dst, value, x, y, size=32, color=INK, weight="regular", opacity=1, align="left"):
    tile = letters(value, size, color, weight)
    if align == "center":
        x -= tile.width / 2
    elif align == "right":
        x -= tile.width
    paste(dst, tile, x, y, opacity)


def rise(dst, value, x, y, t, delay=0, size=32, color=INK, weight="regular"):
    a = enter(t, delay)
    text(dst, value, x, y + 25 * (1 - a), size, color, weight, a)


@functools.lru_cache(maxsize=40)
def capture(name, width):
    src = Image.open(args.assets / f"{name}.png").convert("RGBA")
    height = round(src.height * width / src.width)
    src = src.resize((width, height), Image.Resampling.LANCZOS)
    mask = Image.new("L", src.size)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, width - 1, height - 1), radius=16, fill=255)
    src.putalpha(mask)
    return src


@functools.lru_cache(maxsize=24)
def shadow(width, height):
    tile = Image.new("RGBA", (width + 100, height + 100))
    ImageDraw.Draw(tile).rounded_rectangle((50, 50, width + 50, height + 50), radius=18, fill=(0, 0, 0, 45))
    return tile.filter(ImageFilter.GaussianBlur(22))


def framed(dst, src, x, y, alpha=1, border="#435064"):
    paste(dst, shadow(*src.size), x - 50, y - 35, alpha)
    paste(dst, src, x, y, alpha)
    edge = Image.new("RGBA", src.size)
    ImageDraw.Draw(edge).rounded_rectangle((0, 0, src.width - 1, src.height - 1), radius=16, outline=border, width=2)
    paste(dst, edge, x, y, alpha)


def common(dst, scene, t, dark=False):
    ink, muted, accent = (WHITE, MUTED, ACCENT) if dark else (INK, "#71665F", RED)
    rule = "#3D4A5E" if dark else "#DED6C9"
    paste(dst, logo, 108, 66)
    text(dst, "SHIREI", 178, 79, 31, ink, "bold")
    text(dst, "NATIVE GO INTERFACES", 330, 86, 19, muted, "medium")
    text(dst, "FLEXBOX-STYLE LAYOUT", 1810, 85, 22, accent, "medium", align="right")
    d = ImageDraw.Draw(dst)
    d.line((110, 985, 1810, 985), fill=rule, width=2)
    text(dst, ("FIND YOUR STRUCTURE", "CHOOSE A DIRECTION", "SHARE THE SPACE", "LET IT FLEX")[scene],
         110, 1011, 18, muted, "medium")
    text(dst, f"0{scene+1} / 04", 1810, 1010, 20, ink, "mono", align="right")
    for i in range(4):
        x = 803+i*82
        d.line((x, 1025, x+60, 1025), fill=rule, width=3)
        p = 1 if i < scene else min(1, t/6) if i == scene else 0
        if p:
            d.line((x, 1025, x+60*p, 1025), fill=accent, width=3)


def pill(dst, value, x, y, opacity=1, dark=False):
    color = ACCENT if dark else RED
    width = round(font(25, "mono").getlength(value))+44
    tile = Image.new("RGBA", (width, 64))
    ImageDraw.Draw(tile).rounded_rectangle((0, 0, width-1, 63), radius=10,
                                          fill="#303C4D" if dark else "#E9E0D3")
    text(tile, value, 22, 20, 25, color, "mono")
    paste(dst, tile, x, y, opacity)


def guide(dst, x1, y, x2, value, color=MUTED):
    d = ImageDraw.Draw(dst)
    d.line((x1, y, x2, y), fill=color, width=2)
    for x in (x1, x2):
        d.line((x, y-7, x, y+7), fill=color, width=2)
    text(dst, value, (x1+x2)/2, y+20, 20, color, "mono", align="center")


def scene_frame(scene, t):
    if scene == 0:
        dst = Image.new("RGBA", (W, H), PAPER)
        common(dst, scene, t)
        rise(dst, "FLEXIBLE BY DESIGN", 110, 224, t, .05, 21, RED, "medium")
        rise(dst, "A place for", 100, 313, t, .15, 113, INK, "heavy")
        rise(dst, "everything.", 100, 449, t, .35, 113, RED, "heavy")
        rise(dst, "Rows. Columns. Flexible space.", 110, 620, t, .6, 32, INK)
        rise(dst, "Compose your interface in Go.", 110, 672, t, .75, 32, "#71665F")
        src = capture("app-820", 860)
        x, y = 950, 314
        text(dst, "ONE TREE. A WHOLE INTERFACE.", x, 267, 19, "#71665F", "medium", enter(t, .6))
        paste(dst, shadow(*src.size), x-50, y-35, enter(t, 1.3))
        header = round(src.height*52/440)
        side = round(src.width*136/820)
        for i, (box, dx, dy) in enumerate((( (0, 0, src.width, header), 0, -65),
                                           ((0, header, side, src.height), -75, 0),
                                           ((side, header, src.width, src.height), 75, 35))):
            a = enter(t, .6+i*.45, 1)
            part = src.crop(box)
            paste(dst, part, x+box[0]+dx*(1-a), y+box[1]+dy*(1-a), a)
        for i, value in enumerate(("Row", "Grow", "Expand")):
            pill(dst, value, 950+i*285, 828, enter(t, 2.6+i*.25))
        return dst

    if scene == 1:
        dst = Image.new("RGBA", (W, H), PAPER)
        common(dst, scene, t)
        rise(dst, "Change direction.", 104, 260, t, .1, 83, INK, "heavy")
        rise(dst, "Keep the structure.", 104, 369, t, .3, 76, RED, "heavy")
        rise(dst, "Arrange across. Stack down.", 110, 521, t, .55, 32, INK)
        rise(dst, "Nest containers to build the whole.", 110, 576, t, .7, 30, "#71665F")
        p = enter(t, 2.2, 1.25)
        x, y, scale = 1050, 265, 1.68
        layer = Image.new("RGBA", (780, 650))
        ld = ImageDraw.Draw(layer)
        ld.rounded_rectangle((0, 0, 739, 604), radius=18, fill="#EFE8DD", outline="#D8CCBC", width=2)
        for i, (row, col) in enumerate(zip(geometry["row"], geometry["column"])):
            rx, ry = row["Origin"]
            cx, cy = col["Origin"]
            w, h = row["Size"]
            ox, oy = (rx+(cx-rx)*p)*scale, (ry+(cy-ry)*p)*scale
            ld.rounded_rectangle((ox, oy, ox+w*scale, oy+h*scale), radius=13, fill=COLORS[i])
            text(layer, f"0{i+1}", ox+w*scale/2, oy+h*scale/2-22, 40, WHITE, "bold", align="center")
        paste(dst, layer, x, y, enter(t, .4))
        label = "Row" if p < .5 else "Column (default)"
        text(dst, label, 1420, 893, 25, RED, "mono", enter(t, .7), align="center")
        pill(dst, "Gap(16)  ·  Center", 110, 742, enter(t, 1))
        return dst

    if scene == 2:
        dst = Image.new("RGBA", (W, H), DARK)
        common(dst, scene, t, True)
        rise(dst, "Give space a purpose.", 104, 190, t, .05, 86, WHITE, "bold")
        rise(dst, "Grow shares the space that remains.", 110, 309, t, .25, 32, MUTED)
        # Each width uses rectangles computed by the Shirei layout engine.
        p = enter(t, 1, 1.7)-enter(t, 4, 1.4)*.65
        width = 560+4*round(60*p)
        x, y, scale = (W-width*2)/2, 446, 2
        layer = Image.new("RGBA", (W, H))
        ld = ImageDraw.Draw(layer)
        ld.rounded_rectangle((x, y, x+width*scale, y+360), radius=18,
                             fill="#253246", outline="#4E5C70", width=2)
        for i, r in enumerate(geometry[f"grow-{width}"]):
            rx, ry = r["Origin"]
            w, h = r["Size"]
            ox, oy = x+rx*scale, y+ry*scale
            ld.rounded_rectangle((ox, oy, ox+w*scale, oy+h*scale), radius=12, fill=COLORS[i])
            text(layer, f"Grow({2 if i == 1 else 1})", ox+w*scale/2, oy+58, 33, WHITE, "mono", align="center")
            text(layer, f"+{w-80:.0f} extra", ox+w*scale/2, oy+127, 23, WHITE, "mono", align="center")
            # The short rule shows the same minimum width in every child.
            ld.line((ox, oy+h*scale-27, ox+80*scale, oy+h*scale-27), fill=WHITE, width=5)
        guide(layer, x, 402, x+width*scale, f"{width} pt container", MUTED)
        paste(dst, layer, 0, 0, enter(t, .45))
        text(dst, "Equal minimum widths. Extra space split 1 : 2 : 1.", 960, 868,
             27, MUTED, opacity=enter(t, .9), align="center")
        return dst

    dst = Image.new("RGBA", (W, H), DARK)
    common(dst, scene, t, True)
    rise(dst, "Build the structure.", 104, 244, t, .1, 78, WHITE, "heavy")
    rise(dst, "Let it flex.", 104, 358, t, .3, 106, ACCENT, "heavy")
    rise(dst, "Fixed where it matters.", 110, 519, t, .55, 32, WHITE)
    rise(dst, "Flexible where it counts.", 110, 573, t, .7, 32, MUTED)
    # Captures keep a constant display scale as the actual viewport resizes.
    p = 1-enter(t, .9, 1.65)+enter(t, 3.25, 1.75)
    width = 580+4*round(60*p)
    scale = 1.06
    sw = round(width*scale)
    x, y = 1810-sw, 280
    shot = capture(f"app-{width}", sw)
    framed(dst, shot, x, y, enter(t, .3), "#4E5C70")
    text(dst, "ACTUAL SHIREI LAYOUT / RESIZING", 1810, 235, 18, MUTED, "medium",
         enter(t, .3), align="right")
    if t > .5:
        guide(dst, x, 790, x+136*scale, "Fixed", ACCENT)
        guide(dst, x+136*scale+12, 790, 1810, "Grow(1)", MUTED)
        d = ImageDraw.Draw(dst)
        d.rounded_rectangle((x-9, y+210, x-3, y+250), radius=3, fill=ACCENT)
    button = Image.new("RGBA", (440, 84))
    bd = ImageDraw.Draw(button)
    bd.rounded_rectangle((0, 0, 439, 83), radius=8, fill=WHITE)
    text(button, "Build with Shirei", 28, 26, 28, DARK, "medium")
    bd.line((369, 54, 398, 25), fill=RED, width=3)
    bd.line((376, 25, 398, 25, 398, 47), fill=RED, width=3)
    paste(dst, button, 110, 751+22*(1-enter(t, 1.35)), enter(t, 1.35))
    text(dst, "judi.systems/shirei", 110, 870, 27, ACCENT, opacity=enter(t, 1.65))
    return dst


def frame_at(t):
    scene = min(3, int(t/6))
    local = t-scene*6
    frame = scene_frame(scene, local+(.4 if scene else 0))
    if scene < 3 and local > 5.6:
        following = scene_frame(scene+1, local-5.6)
        frame = Image.blend(frame, following, ease((local-5.6)/.4))
    return frame.convert("RGB")


def main():
    args.output.mkdir(parents=True, exist_ok=True)
    times = (1.7, 4.6, 7.6, 10.6, 12.9, 15.2, 20.6, 23.3)
    contact = Image.new("RGB", (1280, 1536), PAPER)
    for i, t in enumerate(times):
        frame = frame_at(t)
        frame.save(args.output / f"frame-{t:04.1f}.png")
        contact.paste(frame.resize((640, 360), Image.Resampling.LANCZOS), ((i%2)*640, (i//2)*384))
        ImageDraw.Draw(contact).text(((i%2)*640+12, (i//2)*384+361), f"{t:.1f}s", fill=INK, font=font(15))
    contact.save(args.output / "storyboard.jpg", quality=92)
    frame_at(4.6).save(args.output / "poster.jpg", quality=95)
    if args.preview:
        print(args.output / "storyboard.jpg", flush=True)
        return
    path = args.output / "shirei-layout.mp4"
    command = ["ffmpeg", "-hide_banner", "-loglevel", "warning", "-y",
               "-f", "rawvideo", "-pixel_format", "rgb24", "-video_size", f"{W}x{H}",
               "-framerate", str(FPS), "-i", "pipe:0", "-an", "-c:v", "libx264",
               "-preset", "medium", "-crf", "18", "-pix_fmt", "yuv420p", "-movflags", "+faststart",
               "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", str(path)]
    with subprocess.Popen(command, stdin=subprocess.PIPE) as process:
        try:
            for index in range(FPS*DURATION):
                process.stdin.write(frame_at(index/FPS).tobytes())
                if index % (FPS*3) == 0:
                    print(f"Rendering {index/FPS:.0f}s / {DURATION}s", flush=True)
            process.stdin.close()
            if process.wait() != 0:
                raise RuntimeError("ffmpeg encoding failed")
        except BaseException:
            process.kill()
            raise
    print(path, flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--assets", type=Path, default=ROOT / "output/assets")
    parser.add_argument("--output", type=Path, default=ROOT / "output")
    parser.add_argument("--font", default="/System/Library/Fonts/Avenir Next.ttc")
    parser.add_argument("--mono", default="/System/Library/Fonts/Menlo.ttc")
    parser.add_argument("--preview", action="store_true")
    args = parser.parse_args()
    logo = Image.open(ROOT.parents[2] / "static-sites/judi.systems/shirei/shirei-icon.webp").convert("RGBA")
    logo = logo.resize((54, 54), Image.Resampling.LANCZOS)
    geometry = json.loads((args.assets / "geometry.json").read_text())
    main()
