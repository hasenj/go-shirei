"""Render the 24-second dark-mode promo from actual Shirei palette captures.

Requires Pillow and ffmpeg. --preview produces a storyboard without encoding.
"""

import argparse
import colorsys
import functools
import json
import math
import subprocess
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont


ROOT = Path(__file__).resolve().parent
W, H, FPS, DURATION = 1920, 1080, 30, 24
PAPER, INK, RED = "#F6F2E9", "#211C19", "#8A1F1F"
NAVY, WHITE, BLUE = "#151B25", "#F0F3F7", "#86BBF4"
WARM, BLUSH = "#231B18", "#E6B7A7"


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


def compare(light, dark, amount):
    if amount <= 0:
        return light
    if amount >= 1:
        return dark
    width, height = light.size
    edge = (width + 160) * amount - 80
    mask = Image.new("L", light.size)
    ImageDraw.Draw(mask).polygon(((0, 0), (edge + 80, 0), (edge - 80, height), (0, height)), fill=255)
    out = Image.composite(dark, light, mask)
    line = Image.new("RGBA", light.size)
    ImageDraw.Draw(line).line((edge + 80, 0, edge - 80, height), fill=(134, 187, 244, 190), width=2)
    out.alpha_composite(line)
    out.putalpha(light.getchannel("A"))
    return out


def sun(dst, cx, cy, r, color, angle=0):
    d = ImageDraw.Draw(dst)
    d.ellipse((cx-r*.48, cy-r*.48, cx+r*.48, cy+r*.48), outline=color, width=3)
    for i in range(8):
        a = angle + i * math.pi / 4
        d.line((cx + math.cos(a)*r*.75, cy + math.sin(a)*r*.75,
                cx + math.cos(a)*r, cy + math.sin(a)*r), fill=color, width=3)


def moon(dst, cx, cy, r, color):
    layer = Image.new("RGBA", (round(r * 3), round(r * 3)))
    d = ImageDraw.Draw(layer)
    c = r * 1.5
    d.ellipse((c-r, c-r, c+r, c+r), fill=color)
    d.ellipse((c-r*.35, c-r*1.25, c+r*1.35, c+r*.45), fill=(0, 0, 0, 0))
    paste(dst, layer, cx-c, cy-c)


def common(dst, scene, t, dark=False, warm=False):
    ink = WHITE if dark else INK
    muted = BLUSH if warm else "#A6B2C3" if dark else "#71665F"
    accent = BLUSH if warm else BLUE if dark else RED
    rule = "#483831" if warm else "#344050" if dark else "#DED6C9"
    paste(dst, logo, 108, 66)
    text(dst, "SHIREI", 178, 79, 31, ink, "bold")
    text(dst, "NATIVE GO INTERFACES", 330, 86, 19, muted, "medium")
    text(dst, "DARK MODE", 1810, 85, 22, accent, "medium", align="right")
    d = ImageDraw.Draw(dst)
    d.line((110, 985, 1810, 985), fill=rule, width=2)
    label = ("A CHANGE OF LIGHT", "EVERY DETAIL", "YOUR PALETTE", "YOUR APPEARANCE")[scene]
    text(dst, label, 110, 1011, 18, muted, "medium")
    text(dst, f"0{scene+1} / 04", 1810, 1010, 20, ink, "mono", align="right")
    for i in range(4):
        x = 803 + i * 82
        d.line((x, 1025, x+60, 1025), fill=rule, width=3)
        p = 1 if i < scene else min(1, t/6) if i == scene else 0
        if p:
            d.line((x, 1025, x+60*p, 1025), fill=accent, width=3)


def opening(t, dark):
    dst = Image.new("RGBA", (W, H), NAVY if dark else PAPER)
    ink, accent = (WHITE, BLUE) if dark else (INK, RED)
    muted = "#A6B2C3" if dark else "#71665F"
    common(dst, 0, t, dark)
    rise(dst, "A DIFFERENT LIGHT. THE SAME SHIREI.", 110, 225, t, .05, 20, accent, "medium")
    rise(dst, "Light.", 100, 302, t, .15, 126, ink, "heavy")
    rise(dst, "Dark.", 100, 445, t, .3, 126, accent, "heavy")
    rise(dst, "Your choice.", 100, 594, t, .45, 92, ink, "heavy")
    rise(dst, "One interface. Both appearances.", 110, 742, t, .7, 29, muted)
    p = enter(t, .25, 1.1)
    x, y = 895 + 60*(1-p), 300
    shot = capture("cool-dark-app" if dark else "cool-light-app", 915)
    framed(dst, shot, x, y, p, "#435064" if dark else "#D8D5CF")
    text(dst, "ACTUAL SHIREI CONTROLS", x, 257, 19, muted, "medium", p)
    progress = enter(t, 1.85, .65)
    d = ImageDraw.Draw(dst)
    d.rounded_rectangle((110, 828, 296, 900), radius=36, fill="#273A52" if dark else "#E2D9CD")
    sun(dst, 147, 864, 19, accent, t * .2)
    moon(dst, 260, 864, 15, accent)
    cx = 148 + progress * 111
    d.ellipse((cx-29, 835, cx+29, 893), fill=WHITE if dark else "#FFFFFF")
    if progress < .5:
        sun(dst, cx, 864, 16, INK, t * .2)
    else:
        moon(dst, cx, 864, 14, "#284C74")
    text(dst, "DARK MODE" if dark else "LIGHT MODE", 327, 850, 22, accent, "medium")
    return dst


def scene_frame(scene, t):
    if scene == 0:
        p = enter(t, 2.05, 1.55)
        if p <= 0:
            return opening(t, False)
        if p >= 1:
            return opening(t, True)
        # The appearance change expands outward from the switch.
        light, dark = opening(t, False), opening(t, True)
        mask = Image.new("L", (W, H))
        r = 2020 * p
        ImageDraw.Draw(mask).ellipse((260-r, 864-r, 260+r, 864+r), fill=255)
        return Image.composite(dark, light, mask)

    if scene == 1:
        dst = Image.new("RGBA", (W, H), NAVY)
        common(dst, scene, t, True)
        rise(dst, "Every detail, in tune.", 104, 199, t, .05, 88, WHITE, "bold")
        rise(dst, "Surfaces, text and controls share the same palette.", 110, 318, t, .25, 32, "#A6B2C3")
        p = enter(t, .4)
        shot = compare(capture("cool-light-controls", 940), capture("cool-dark-controls", 940), enter(t, 1.1, 2))
        framed(dst, shot, 110, 406 + 25*(1-p), p)
        points = (("Surfaces & text", "A foreground for every background.", 449, 1),
                  ("Inputs & selections", "Keep your place as colors change.", 623, 1.5),
                  ("Buttons & accents", "Primary actions stay distinct.", 797, 2))
        for title, caption, y, delay in points:
            a = enter(t, delay)
            d = ImageDraw.Draw(dst)
            if a > 0:
                d.line((1102, y+16, 1146, y+16), fill=BLUE, width=2)
                d.ellipse((1097, y+11, 1107, y+21), fill=BLUE)
            rise(dst, title, 1180, y, t, delay, 36, WHITE, "medium")
            rise(dst, caption, 1180, y+64, t, delay+.15, 25, "#A6B2C3")
        return dst

    if scene == 2:
        dst = Image.new("RGBA", (W, H), PAPER)
        common(dst, scene, t)
        rise(dst, "Pick your palette.", 104, 191, t, .05, 90, INK, "bold")
        rise(dst, "Ready-made palettes. Yours to customize.", 110, 311, t, .25, 32, "#71665F")
        for i, name in enumerate(("cool-light", "cool-dark", "warm-light", "warm-dark")):
            x, y = (110 if i % 2 == 0 else 1050), (402 if i < 2 else 712)
            delay = .4 + i*.24
            a = enter(t, delay)
            rise(dst, name.upper().replace("-", " / "), x+2, y-42, t, delay, 21, INK, "medium")
            shot = capture(name+"-palette", 760)
            framed(dst, shot, x, y+25*(1-a), a, "#CFC5B7")
            swatch = colors[name]["accent"]
            d = ImageDraw.Draw(dst)
            if a > 0:
                d.ellipse((x+735, y-36, x+753, y-18), fill=swatch)
            if name == "warm-dark" and t > 3:
                pulse = enter(t, 3, 1)
                outline = Image.new("RGBA", (776, shot.height+16))
                ImageDraw.Draw(outline).rounded_rectangle((1, 1, 774, shot.height+14), radius=23,
                                                          outline=colors[name]["focus"], width=3)
                paste(dst, outline, x-8, y-8, pulse)
        return dst

    dst = Image.new("RGBA", (W, H), WARM)
    common(dst, scene, t, True, True)
    rise(dst, "Your app.", 102, 251, t, .1, 116, PAPER, "heavy")
    rise(dst, "Your atmosphere.", 102, 390, t, .3, 91, PAPER, "heavy")
    rise(dst, "Follow system appearance.", 110, 545, t, .6, 32, BLUSH)
    rise(dst, "Or choose your own.", 110, 595, t, .75, 32, BLUSH)
    # The system selector illustrates the application's follow-system choice.
    selector = Image.new("RGBA", (760, 70))
    sd = ImageDraw.Draw(selector)
    sd.rounded_rectangle((0, 0, 759, 69), radius=12, fill="#382A25", outline="#5B463B", width=2)
    sd.rounded_rectangle((6, 6, 249, 63), radius=8, fill="#665044")
    for i, title in enumerate(("System", "Light", "Dark")):
        text(selector, title, 126+i*253, 23, 25, PAPER if i == 0 else BLUSH, "medium", align="center")
    paste(dst, selector, 1050, 274, enter(t, .35))
    change = enter(t, 1.6, 1.3)
    shot = compare(capture("warm-light-app", 760), capture("warm-dark-app", 760), change)
    framed(dst, shot, 1050, 390, enter(t, .55), "#5B463B")
    code = "SetDarkMode(darkmode.OSDarkMode())"
    n = round(len(code) * min(1, max(0, (t-1)/1.4)))
    text(dst, code[:n], 1430, 910, 23, BLUSH, "mono", align="center")
    button = Image.new("RGBA", (440, 84))
    bd = ImageDraw.Draw(button)
    bd.rounded_rectangle((0, 0, 439, 83), radius=8, fill=PAPER)
    text(button, "Build with Shirei", 28, 26, 28, WARM, "medium")
    bd.line((369, 54, 398, 25), fill=RED, width=3)
    bd.line((376, 25, 398, 25, 398, 47), fill=RED, width=3)
    paste(dst, button, 110, 751 + 22*(1-enter(t, 1.35)), enter(t, 1.35))
    text(dst, "judi.systems/shirei", 110, 870, 27, BLUSH, opacity=enter(t, 1.65))
    return dst


def frame_at(t):
    scene = min(3, int(t/6))
    local = t - scene*6
    frame = scene_frame(scene, local + (.4 if scene else 0))
    if scene < 3 and local > 5.6:
        following = scene_frame(scene+1, local-5.6)
        frame = Image.blend(frame, following, ease((local-5.6)/.4))
    return frame.convert("RGB")


def main():
    args.output.mkdir(parents=True, exist_ok=True)
    times = (1.7, 2.6, 4.6, 7.6, 10.6, 16.7, 19.2, 23.3)
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
    path = args.output / "shirei-darkmode.mp4"
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
    raw_colors = json.loads((args.assets / "palettes.json").read_text())
    colors = {name: {role: tuple(round(v*255) for v in colorsys.hls_to_rgb(h/360, l/100, s/100))
                     for role, (h, s, l, a) in palette.items()} for name, palette in raw_colors.items()}
    main()
