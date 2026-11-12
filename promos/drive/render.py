"""Render a 24-second Drive promo from a captured live application session.

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
DARK, WHITE, MINT = "#142824", "#F4F3EB", "#9BDEC1"
MUTED = "#AAC1B8"


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
    ink, muted, accent = (WHITE, MUTED, MINT) if dark else (INK, "#71665F", RED)
    rule = "#3A5148" if dark else "#DED6C9"
    paste(dst, logo, 108, 66)
    text(dst, "SHIREI", 178, 79, 31, ink, "bold")
    text(dst, "NATIVE GO INTERFACES", 330, 86, 19, muted, "medium")
    text(dst, "DRIVE", 1810, 85, 22, accent, "medium", align="right")
    d = ImageDraw.Draw(dst)
    d.line((110, 985, 1810, 985), fill=rule, width=2)
    text(dst, ("TAKE THE CONTROLS", "INSPECT", "INTERACT", "VERIFY")[scene],
         110, 1011, 18, muted, "medium")
    text(dst, f"0{scene+1} / 04", 1810, 1010, 20, ink, "mono", align="right")
    for i in range(4):
        x = 803 + i * 82
        d.line((x, 1025, x+60, 1025), fill=rule, width=3)
        p = 1 if i < scene else min(1, t/6) if i == scene else 0
        if p:
            d.line((x, 1025, x+60*p, 1025), fill=accent, width=3)


def terminal(width, height, title):
    tile = Image.new("RGBA", (width, height))
    d = ImageDraw.Draw(tile)
    d.rounded_rectangle((0, 0, width-1, height-1), radius=16, fill=DARK, outline="#496358", width=2)
    d.line((1, 57, width-2, 57), fill="#3A5148", width=1)
    for i, color in enumerate(("#6B8B7D", "#8CAC98", MINT)):
        x = 28+i*22
        d.ellipse((x, 24, x+9, 33), fill=color)
    text(tile, title, width-26, 21, 17, MUTED, "medium", align="right")
    return tile


def type_line(dst, value, x, y, t, start, duration=.8, size=26, color=MINT):
    p = min(1, max(0, (t-start)/duration))
    n = round(len(value)*p)
    text(dst, value[:n], x, y, size, color, "mono")
    if start < t < start+duration+.25:
        cx = x+font(size, "mono").getlength(value[:n])+4
        ImageDraw.Draw(dst).rectangle((cx, y, cx+2, y+size), fill=color)


def target_rect(name, x, y, width):
    node = session["Nodes"][name]
    ox, oy = node["Rect"]["Origin"]
    w, h = node["Rect"]["Size"]
    scale = width/640
    return x+ox*scale, y+oy*scale, w*scale, h*scale


def highlight(dst, name, x, y, width, opacity=1, caption=True):
    ox, oy, w, h = target_rect(name, x, y, width)
    overlay = Image.new("RGBA", (W, H))
    d = ImageDraw.Draw(overlay)
    d.rounded_rectangle((ox-7, oy-7, ox+w+7, oy+h+7), radius=8,
                        fill=(138, 31, 31, 12), outline=RED, width=3)
    if caption:
        d.rounded_rectangle((ox-7, oy-45, ox+115, oy-12), radius=5, fill=RED)
        text(overlay, name, ox+5, oy-40, 19, WHITE, "mono")
    paste(dst, overlay, 0, 0, opacity)


def cursor(dst, x, y, opacity=1, click=0):
    tile = Image.new("RGBA", (90, 90))
    d = ImageDraw.Draw(tile)
    if click > 0:
        r = 10+32*click
        d.ellipse((45-r, 45-r, 45+r, 45+r), outline=(138, 31, 31, round(220*(1-click))), width=3)
    d.polygon(((45, 45), (45, 74), (53, 66), (60, 80), (66, 77), (59, 63), (70, 63)),
              fill=WHITE, outline=INK, width=2)
    paste(dst, tile, x-45, y-45, opacity)


def scene_frame(scene, t):
    if scene == 0:
        dst = Image.new("RGBA", (W, H), PAPER)
        common(dst, scene, t)
        rise(dst, "MEET THE DRIVE INTERFACE", 110, 224, t, .05, 21, RED, "medium")
        rise(dst, "Put your UI", 100, 306, t, .15, 112, INK, "heavy")
        rise(dst, "in motion.", 100, 442, t, .35, 112, RED, "heavy")
        rise(dst, "A native app, ready for", 110, 609, t, .6, 33, INK)
        rise(dst, "scripts and agents.", 110, 660, t, .75, 33, INK)
        a = enter(t, .4, 1)
        x, y = 950+60*(1-a), 300
        framed(dst, capture("initial", 860), x, y, a, "#CFC5B7")
        text(dst, "ACTUAL SHIREI APP", x, 253, 19, "#71665F", "medium", a)
        term = terminal(700, 150, "LOOPBACK / UDP")
        type_line(term, "> ping", 28, 80, t, 1.35, .65, 28)
        text(term, session["Replies"]["ping"], 296, 80, 28, WHITE, "mono", enter(t, 2.3, .3))
        if t > 2.3:
            td = ImageDraw.Draw(term)
            td.line((205, 96, 257, 96), fill=MINT, width=2)
            td.line((248, 88, 257, 96, 248, 104), fill=MINT, width=2)
        paste(dst, term, 110, 769, enter(t, 1))
        for i, label in enumerate(("INSPECT", "INTERACT", "VERIFY")):
            tx = 950+i*286
            p = enter(t, 2.7+i*.35)
            text(dst, f"0{i+1}", tx, 884, 20, RED, "mono", p)
            text(dst, label, tx+47, 883, 20, INK, "medium", p)
        return dst

    if scene == 1:
        dst = Image.new("RGBA", (W, H), PAPER)
        common(dst, scene, t)
        rise(dst, "See what’s there.", 104, 191, t, .05, 88, INK, "bold")
        rise(dst, "Names. Roles. Bounds. Real application state.", 110, 309, t, .25, 32, "#71665F")
        term = terminal(740, 462, "DRIVE / QUERY")
        type_line(term, "> query save", 30, 89, t, .55, .75, 29)
        for i, line in enumerate(session["Replies"]["query-save"].splitlines()):
            color = MINT if line.startswith("path:") else WHITE
            text(term, line, 30, 151+i*39, 25, color, "mono", enter(t, 1.4+i*.08, .35))
        paste(dst, term, 110, 413, enter(t, .35))
        framed(dst, capture("initial", 860), 950, 380, enter(t, .45), "#CFC5B7")
        text(dst, "LIVE APP / NAMED TARGET", 950, 342, 18, "#71665F", "medium", enter(t, .5))
        highlight(dst, "save", 950, 380, 860, enter(t, 2.05))
        # A response travels back from the highlighted component.
        if t > 2:
            p = min(1, (t-2)/.9)
            d = ImageDraw.Draw(dst)
            d.line((864, 657, 930, 657), fill="#C8B8A9", width=2)
            cx = 930-66*p
            d.ellipse((cx-5, 652, cx+5, 662), fill=RED)
        return dst

    if scene == 2:
        dst = Image.new("RGBA", (W, H), DARK)
        common(dst, scene, t, True)
        rise(dst, "Say it. See it happen.", 104, 191, t, .05, 87, WHITE, "bold")
        rise(dst, "Type into fields. Click named controls.", 110, 309, t, .2, 32, MUTED)
        term = terminal(740, 468, "GO / SHIREI/DRIVE")
        type_line(term, 'drive.Type(port, "name",', 30, 99, t, .4, .7, 27)
        type_line(term, '    "Night garden")', 30, 144, t, .95, .55, 27)
        type_line(term, 'drive.Click(port, "save")', 30, 262, t, 3.2, .7, 27)
        text(term, "INPUT SENT", 31, 210, 18, MUTED, "medium", enter(t, 2.9))
        text(term, "ACTION COMPLETE", 31, 334, 18, MINT, "medium", enter(t, 4.3))
        paste(dst, term, 110, 414, enter(t, .25))
        typed = min(12, max(0, int((t-1.5)/1.5*12)))
        saved = t >= 4.2
        shot = "saved" if saved else f"type-{typed:02d}"
        framed(dst, capture(shot, 860), 950, 380, enter(t, .35), "#526C60")
        text(dst, "LIVE APP / REAL INPUT", 950, 342, 18, MUTED, "medium", enter(t, .4))
        ix, iy, iw, ih = target_rect("name", 950, 380, 860)
        bx, by, bw, bh = target_rect("save", 950, 380, 860)
        p = enter(t, 3.35, .65)
        cx = ix+iw*.4+(bx+bw*.5-ix-iw*.4)*p
        cy = iy+ih*.5+(by+bh*.5-iy-ih*.5)*p
        cursor(dst, cx, cy, enter(t, 1.15), min(1, (t-4.12)/.6) if 4.12<t<4.72 else 0)
        if saved:
            highlight(dst, "status", 950, 380, 860, enter(t, 4.2), False)
        return dst

    dst = Image.new("RGBA", (W, H), DARK)
    common(dst, scene, t, True)
    rise(dst, "Built for scripts.", 104, 242, t, .1, 91, WHITE, "heavy")
    rise(dst, "Ready for agents.", 104, 363, t, .3, 91, MINT, "heavy")
    rise(dst, "Inspect. Interact. Verify.", 110, 523, t, .55, 35, WHITE, "medium")
    rise(dst, "Loopback commands. Go helpers.", 110, 587, t, .7, 29, MUTED)
    framed(dst, capture("saved", 760), 1050, 230, enter(t, .3), "#526C60")
    text(dst, "CAPTURE THE RESULT", 1050, 186, 18, MUTED, "medium", enter(t, .3))
    term = terminal(800, 264, "DRIVE / VERIFY")
    type_line(term, "> show status", 26, 77, t, .7, .65, 24)
    lines = [line for line in session["Replies"]["saved-status"].splitlines()
             if line.startswith(("path:", "role:", "value:"))]
    for i, line in enumerate(lines):
        text(term, line, 26, 120+i*34, 24, MINT if i == 2 else WHITE,
             "mono", enter(t, 1.5+i*.15, .4))
    paste(dst, term, 1010, 665, enter(t, .65))
    a = enter(t, 2)
    badge = Image.new("RGBA", (214, 48))
    ImageDraw.Draw(badge).rounded_rectangle((0, 0, 213, 47), radius=24, fill=MINT)
    text(badge, "STATE VERIFIED", 107, 15, 18, DARK, "bold", align="center")
    paste(dst, badge, 1510, 886, a)
    button = Image.new("RGBA", (440, 84))
    bd = ImageDraw.Draw(button)
    bd.rounded_rectangle((0, 0, 439, 83), radius=8, fill=WHITE)
    text(button, "Build with Shirei", 28, 26, 28, DARK, "medium")
    bd.line((369, 54, 398, 25), fill=RED, width=3)
    bd.line((376, 25, 398, 25, 398, 47), fill=RED, width=3)
    paste(dst, button, 110, 751+22*(1-enter(t, 1.35)), enter(t, 1.35))
    text(dst, "judi.systems/shirei", 110, 870, 27, MINT, opacity=enter(t, 1.65))
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
    times = (1.7, 4.6, 7.6, 10.6, 14.7, 16.7, 19.2, 23.3)
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
    path = args.output / "shirei-drive.mp4"
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
    session = json.loads((args.assets / "session.json").read_text())
    main()
