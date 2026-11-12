"""Render a 24-second furigana promo using text layers exported by Shirei.

Requires Python with Pillow and ffmpeg. The default fonts are macOS system
fonts; --font and --mono accept alternative font files.
"""

import argparse
import functools
import subprocess
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw, ImageFilter, ImageFont


ROOT = Path(__file__).resolve().parent
W, H, FPS, DURATION = 1920, 1080, 30, 24
PAPER = "#F6F2E9"
INK = "#211C19"
RED = "#8A1F1F"
MUTED = "#71665F"
LINE = "#DED6C9"
DARK = "#281714"
LIGHT_RED = "#EDACA0"


def ease(v):
    v = max(0.0, min(1.0, v))
    return 1 - (1 - v) ** 3


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
def text_bitmap(value, size, color=INK, weight="regular"):
    face = font(size, weight)
    bounds = face.getbbox(value)
    tile = Image.new("RGBA", (max(1, bounds[2] - bounds[0] + 6), bounds[3] - bounds[1] + 6))
    ImageDraw.Draw(tile).text((3 - bounds[0], 3 - bounds[1]), value, font=face, fill=color)
    return tile


def paste(dst, src, x, y, opacity=1):
    if opacity <= 0:
        return
    if opacity < .999:
        src = src.copy()
        src.putalpha(src.getchannel("A").point(lambda p: round(p * opacity)))
    dst.alpha_composite(src, (round(x), round(y)))


def text(dst, value, x, y, size=32, color=INK, weight="regular", opacity=1, align="left"):
    tile = text_bitmap(value, size, color, weight)
    if align == "center":
        x -= tile.width / 2
    if align == "right":
        x -= tile.width
    paste(dst, tile, x, y, opacity)


def rising_text(dst, value, x, y, t, delay=0, size=32, color=INK, weight="regular"):
    progress = enter(t, delay)
    text(dst, value, x, y + 30 * (1 - progress), size, color, weight, progress)


@functools.lru_cache(maxsize=32)
def panel(width, height, dark=False):
    tile = Image.new("RGBA", (width + 60, height + 70))
    shadow = Image.new("RGBA", tile.size)
    ImageDraw.Draw(shadow).rounded_rectangle((30, 30, width + 30, height + 30), radius=20, fill=(38, 21, 14, 20))
    tile.alpha_composite(shadow.filter(ImageFilter.GaussianBlur(18)))
    ImageDraw.Draw(tile).rounded_rectangle((30, 20, width + 30, height + 20), radius=20,
                                          fill="#35221E" if dark else "#FFFDFA",
                                          outline="#543630" if dark else LINE, width=2)
    return tile


@functools.lru_cache(maxsize=64)
def glyph_layers(name, width, base_color=INK, reading_color=RED):
    masks = [ImageChops.invert(Image.open(args.assets / f"{name}-{part}.png").convert("L"))
             for part in ("base", "reading")]
    bbox = ImageChops.lighter(*masks).getbbox()
    if bbox is None:
        raise ValueError(f"Empty Shirei glyph export: {name}")
    layers = []
    for mask, color in zip(masks, (base_color, reading_color)):
        mask = mask.crop(bbox)
        size = (width, round(mask.height * width / mask.width))
        mask = mask.resize(size, Image.Resampling.LANCZOS)
        layer = Image.new("RGBA", size, color)
        layer.putalpha(mask)
        layers.append(layer)
    return layers


def japanese(dst, name, cx, cy, width, base_alpha=1, reading_alpha=1,
             reading_rise=0, base_color=INK, reading_color=RED, wipe=1):
    base, reading = glyph_layers(name, width, base_color, reading_color)
    x, y = cx - width / 2, cy - base.height / 2
    paste(dst, base, x, y, base_alpha)
    if wipe < 1:
        reading = reading.crop((0, 0, round(reading.width * max(0, wipe)), reading.height))
    if reading.width:
        paste(dst, reading, x, y - reading_rise, reading_alpha)


def common(dst, scene, t, dark=False):
    color = PAPER if dark else INK
    paste(dst, logo, 108, 66)
    text(dst, "SHIREI", 178, 79, 31, color, "bold")
    text(dst, "NATIVE GO INTERFACES", 330, 86, 19, LIGHT_RED if dark else MUTED, "medium")
    text(dst, "FURIGANA", 1810, 85, 22, LIGHT_RED if dark else RED, "medium", align="right")
    d = ImageDraw.Draw(dst)
    d.line((110, 985, 1810, 985), fill="#543630" if dark else LINE, width=2)
    labels = ("THE READING", "THE SPACING", "THE SAME TEXT", "BUILT INTO SHIREI")
    text(dst, labels[scene], 110, 1011, 18, LIGHT_RED if dark else MUTED, "medium")
    text(dst, f"0{scene + 1} / 04", 1810, 1010, 20, color, "mono", align="right")
    for i in range(4):
        x = 803 + i * 82
        d.line((x, 1025, x + 60, 1025), fill="#543630" if dark else LINE, width=3)
        fraction = 1 if i < scene else min(1, t / 6) if i == scene else 0
        if fraction:
            d.line((x, 1025, x + 60 * fraction, 1025), fill=LIGHT_RED if dark else RED, width=3)


def scene_frame(scene, t):
    dark = scene == 3
    dst = Image.new("RGBA", (W, H), DARK if dark else PAPER)
    d = ImageDraw.Draw(dst)
    common(dst, scene, t, dark)

    if scene == 0:
        rising_text(dst, "A LITTLE HELP ABOVE THE LINE.", 110, 250, t, .05, 22, RED, "medium")
        rising_text(dst, "Furigana,", 102, 330, t, .2, 130, INK, "heavy")
        rising_text(dst, "built in.", 102, 476, t, .4, 130, RED, "heavy")
        rising_text(dst, "Japanese readings, right where", 110, 665, t, .75, 34, MUTED)
        rising_text(dst, "you need them.", 110, 714, t, .85, 34, MUTED)
        p = enter(t, .15, 1.1)
        card_x = 1030 + 80 * (1 - p)
        paste(dst, panel(780, 470), card_x - 30, 305, p)
        japanese(dst, "hero", card_x + 390, 537, 670, p,
                 enter(t, 1.25, 1.2), 34 * (1 - enter(t, 1.25, 1.2)))
        code = "Ruby(base, reading)"
        n = round(len(code) * min(1, max(0, (t - 2.8) / .9)))
        text(dst, code[:n], 1420, 832, 28, MUTED, "mono", align="center")
        d.line((110, 828, 110 + 160 * enter(t, 1.5, 1.2), 828), fill=RED, width=4)

    elif scene == 1:
        rising_text(dst, "Give every reading room.", 104, 201, t, .05, 86, INK, "bold")
        rising_text(dst, "Natural spacing. Even when readings run long.", 110, 320, t, .25, 32, MUTED)
        p = enter(t, .35)
        paste(dst, panel(1700, 270), 80, 395 + (1 - p) * 40, p)
        japanese(dst, "sentence", 960, 543 + (1 - p) * 40, 1450, p,
                 enter(t, .9), 14 * (1 - enter(t, .9)), wipe=enter(t, .8, 1.65))
        rising_text(dst, "COMPOUNDS STAY TOGETHER", 180, 755, t, 1.8, 21, RED, "medium")
        japanese(dst, "words", 505, 867, 420, enter(t, 2), enter(t, 2.3), 14 * (1 - enter(t, 2.3)))
        d.line((960, 750, 960, 925), fill=LINE, width=2)
        rising_text(dst, "LONG READINGS SHARE SPACE", 1100, 755, t, 2.25, 21, RED, "medium")
        japanese(dst, "neighbors", 1415, 865, 360, enter(t, 2.5), enter(t, 2.8), 14 * (1 - enter(t, 2.8)))

    elif scene == 2:
        rising_text(dst, "Copy. Paste. Still furigana.", 104, 203, t, .05, 82, INK, "bold")
        rising_text(dst, "The reading travels with the text.", 110, 320, t, .25, 34, MUTED)
        text(dst, "LABEL", 142, 458, 21, RED, "medium", enter(t, .45))
        text(dst, "TEXT FIELD", 1102, 458, 21, RED, "medium", enter(t, .6))
        for x, delay in ((110, .45), (1070, .6)):
            p = enter(t, delay)
            paste(dst, panel(740, 235), x - 30, 495 + (1 - p) * 28, p)
        selected = enter(t, 1.15, .4) * (1 - enter(t, 2.5, .5))
        selection = Image.new("RGBA", (660, 142))
        ImageDraw.Draw(selection).rounded_rectangle((0, 0, 659, 141), radius=5, fill=(138, 31, 31, 27))
        paste(dst, selection, 150, 555, selected)
        japanese(dst, "copy", 480, 626, 630, enter(t, .65), enter(t, .65))
        arrow = enter(t, 1.7, .8)
        if arrow:
            end = 895 + 130 * arrow
            d.line((895, 627, end, 627), fill=RED, width=3)
            d.line((end - 12, 616, end, 627, end - 12, 638), fill=RED, width=3)
        paste_alpha = enter(t, 2.7, .6)
        japanese(dst, "copy", 1440, 626, 630, paste_alpha, paste_alpha)
        if t > 1 and (int(t * 2) % 2 == 0 or t < 3.2):
            caret_x = 1778 if paste_alpha > .95 else 1125
            d.line((caret_x, 608, caret_x, 673), fill=RED, width=3)
        for cx, label, delay in ((480, "COPY", 1.3), (1440, "PASTE", 2.8)):
            a = enter(t, delay)
            key = Image.new("RGBA", (144, 48))
            ImageDraw.Draw(key).rounded_rectangle((1, 1, 142, 46), radius=8, fill="#EDE5DA", outline=LINE, width=1)
            text(key, label, 72, 13, 19, RED, "medium", align="center")
            paste(dst, key, cx - 72, 792, a)
        text(dst, "Same Unicode string. Same reading.", 960, 905, 30, MUTED, opacity=enter(t, 3.7), align="center")

    else:
        rising_text(dst, "Furigana.", 102, 252, t, .1, 122, PAPER, "heavy")
        rising_text(dst, "Part of the text.", 102, 394, t, .3, 106, PAPER, "heavy")
        rising_text(dst, "Built into labels and editable text.", 110, 562, t, .65, 32, LIGHT_RED)
        japanese(dst, "hero", 1430, 475, 650, enter(t, .6), enter(t, 1.1),
                 25 * (1 - enter(t, 1.1)), PAPER, LIGHT_RED)
        p = enter(t, 1.35)
        button = Image.new("RGBA", (440, 84))
        ImageDraw.Draw(button).rounded_rectangle((0, 0, 439, 83), radius=8, fill=PAPER)
        text(button, "Explore Shirei", 30, 24, 30, DARK, "medium")
        bd = ImageDraw.Draw(button)
        bd.line((369, 54, 398, 25), fill=RED, width=3)
        bd.line((376, 25, 398, 25, 398, 47), fill=RED, width=3)
        paste(dst, button, 110, 737 + (1 - p) * 22, p)
        text(dst, "judi.systems/shirei", 110, 852, 27, LIGHT_RED, opacity=enter(t, 1.6))
        text(dst, "Unicode in. Furigana out.", 1430, 749, 27, LIGHT_RED,
             opacity=enter(t, 1.8), align="center")
    return dst


def frame_at(t):
    scene = min(3, int(t / 6))
    local = t - scene * 6
    frame = scene_frame(scene, local + (.4 if scene > 0 else 0))
    if scene < 3 and local > 5.6:
        # A short cross-dissolve overlaps the next scene's entrance animation.
        following = scene_frame(scene + 1, local - 5.6)
        frame = Image.blend(frame, following, ease((local - 5.6) / .4))
    return frame.convert("RGB")


def main():
    args.output.mkdir(parents=True, exist_ok=True)
    times = (2.8, 4.8, 7.6, 10.7, 14.0, 16.7, 20.8, 23.4)
    contact = Image.new("RGB", (1280, 4 * 384), PAPER)
    for i, t in enumerate(times):
        frame = frame_at(t)
        frame.save(args.output / f"frame-{t:04.1f}.png")
        contact.paste(frame.resize((640, 360), Image.Resampling.LANCZOS), ((i % 2) * 640, (i // 2) * 384))
        ImageDraw.Draw(contact).text(((i % 2) * 640 + 12, (i // 2) * 384 + 361), f"{t:.1f}s", fill=INK, font=font(15))
    contact.save(args.output / "storyboard.jpg", quality=92)
    frame_at(3.8).save(args.output / "poster.jpg", quality=95)
    if args.preview:
        print(args.output / "storyboard.jpg", flush=True)
        return
    path = args.output / "shirei-furigana.mp4"
    command = ["ffmpeg", "-hide_banner", "-loglevel", "warning", "-y",
               "-f", "rawvideo", "-pixel_format", "rgb24", "-video_size", f"{W}x{H}",
               "-framerate", str(FPS), "-i", "pipe:0", "-an", "-c:v", "libx264",
               "-preset", "medium", "-crf", "18", "-pix_fmt", "yuv420p",
               "-movflags", "+faststart", "-color_primaries", "bt709",
               "-color_trc", "bt709", "-colorspace", "bt709", str(path)]
    with subprocess.Popen(command, stdin=subprocess.PIPE) as process:
        try:
            for index in range(FPS * DURATION):
                process.stdin.write(frame_at(index / FPS).tobytes())
                if index % (FPS * 3) == 0:
                    print(f"Rendering {index / FPS:.0f}s / {DURATION}s", flush=True)
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
    parser.add_argument("--preview", action="store_true", help="render storyboard stills only")
    args = parser.parse_args()
    logo_path = ROOT.parents[2] / "static-sites/judi.systems/shirei/shirei-icon.webp"
    logo = Image.open(logo_path).convert("RGBA").resize((54, 54), Image.Resampling.LANCZOS)
    main()
