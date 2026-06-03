#!/usr/bin/env python3
"""Generate the sx pixel-art wordmark.

Tiled "circuit" style after the reference: solid bright-blue strokes with a
light internal grid (the lines running through the letters), a bright
perimeter outline, and an offset duplicate edge that gives a 3D/embossed
pop. Soft outer glow and a monospace < tagline /> underneath. Transparent
background so it works on light or dark READMEs.
"""
from PIL import Image, ImageDraw, ImageFilter, ImageFont

# --- pixel letterforms (13 wide x 15 tall) ---------------------------------
S = [
    "0111111111110",
    "1111111111111",
    "1110000000000",
    "1110000000000",
    "1111000000000",
    "0111110000000",
    "0011111100000",
    "0001111111000",
    "0000011111100",
    "0000000111110",
    "0000000001111",
    "0000000000111",
    "0000000000111",
    "1111111111111",
    "0111111111110",
]
X = [
    "1100000000011",
    "1110000000111",
    "0111000001110",
    "0011100011100",
    "0011100011100",
    "0001110111000",
    "0000111110000",
    "0000011100000",
    "0000111110000",
    "0001110111000",
    "0011100011100",
    "0011100011100",
    "0111000001110",
    "1110000000111",
    "1100000000011",
]

GAP_COLS = 3   # blank columns between the two letters
LW = 13        # letter width
letters = [S, X]

# --- tagline ---------------------------------------------------------------
TAGLINE = "< your team's AI, everywhere />"
FONT_PATH = "/System/Library/Fonts/SFNSMono.ttf"

# --- geometry --------------------------------------------------------------
SS = 4               # supersample factor for smooth edges
BLOCK = 18 * SS      # cell size (cells abut: no gap, the grid lines are drawn on top)
GRID_W = 2 * SS      # internal grid line width
OUT_W = 3 * SS       # perimeter outline width
OFF = 5 * SS         # 3D offset of the duplicate edge (down-right)
PAD = 36 * SS        # outer padding
TAG_GAP = 30 * SS    # space between wordmark and tagline
FONT_PX = 33 * SS    # tagline size
TRACK = 5 * SS       # tagline letter spacing

rows = 15
cols = LW * len(letters) + GAP_COLS * (len(letters) - 1)
grid_w = cols * BLOCK
grid_h = rows * BLOCK

# --- colors ----------------------------------------------------------------
FILL_TOP = (70, 170, 255)     # lighter blue (top of stroke)
FILL_BOT = (18, 120, 246)     # deeper blue  (bottom of stroke)
GRID = (168, 214, 255)        # light internal grid lines
BRIGHT = (228, 243, 255)      # bright perimeter outline
SHADOW3D = (104, 173, 250)    # offset edge that reads as depth
GLOW = (32, 145, 255)         # glow color
TAG_COL = (42, 157, 244, 255)


def lerp(a, b, t):
    return tuple(round(a[i] + (b[i] - a[i]) * t) for i in range(len(a)))


# --- measure tagline -------------------------------------------------------
font = ImageFont.truetype(FONT_PATH, FONT_PX)
char_w = [font.getlength(ch) for ch in TAGLINE]
tag_w = sum(char_w) + TRACK * (len(TAGLINE) - 1)
asc, desc = font.getmetrics()
tag_h = asc + desc

# --- canvas ----------------------------------------------------------------
content_w = max(grid_w, tag_w)
W = round(content_w + PAD * 2)
H = round(PAD + grid_h + TAG_GAP + tag_h + PAD)

gx = (W - grid_w) // 2          # wordmark x offset
gy = PAD                        # wordmark y offset

# --- collect filled cells (absolute grid column) ---------------------------
cells = []
col_offset = 0
for letter in letters:
    for r, line in enumerate(letter):
        for c, ch in enumerate(line):
            if ch == "1":
                cells.append((col_offset + c, r))
    col_offset += LW + GAP_COLS
FILLED = set(cells)


def has(c, r):
    return (c, r) in FILLED


def box(c, r):
    x0 = gx + c * BLOCK
    y0 = gy + r * BLOCK
    return x0, y0, x0 + BLOCK, y0 + BLOCK


img = Image.new("RGBA", (W, H), (0, 0, 0, 0))


def hline(d, x0, x1, y, w, color):
    d.rectangle([x0 - w / 2, y - w / 2, x1 + w / 2, y + w / 2], fill=color)


def vline(d, y0, y1, x, w, color):
    d.rectangle([x - w / 2, y0 - w / 2, x + w / 2, y1 + w / 2], fill=color)


# --- glow layer ------------------------------------------------------------
glow = Image.new("RGBA", (W, H), (0, 0, 0, 0))
gdraw = ImageDraw.Draw(glow)
for (c, r) in cells:
    x0, y0, x1, y1 = box(c, r)
    gdraw.rectangle([x0, y0, x1, y1], fill=GLOW + (255,))
glow = glow.filter(ImageFilter.GaussianBlur(6 * SS))
glow.putalpha(glow.getchannel("A").point(lambda a: int(a * 0.38)))
img = Image.alpha_composite(img, glow)

draw = ImageDraw.Draw(img)

# --- 1. solid blue fill (cells abut into continuous strokes) ---------------
for (c, r) in cells:
    x0, y0, x1, y1 = box(c, r)
    fill = lerp(FILL_TOP, FILL_BOT, r / (rows - 1)) + (255,)
    draw.rectangle([x0, y0, x1, y1], fill=fill)

# --- 2. light internal grid lines (the lines through the letters) ----------
for (c, r) in cells:
    x0, y0, x1, y1 = box(c, r)
    if has(c + 1, r):
        vline(draw, y0, y1, x1, GRID_W, GRID + (255,))
    if has(c, r + 1):
        hline(draw, x0, x1, y1, GRID_W, GRID + (255,))

# --- 3. offset 3D edge on the down-right boundary --------------------------
for (c, r) in cells:
    x0, y0, x1, y1 = box(c, r)
    if not has(c, r + 1):  # bottom boundary
        hline(draw, x0 + OFF, x1 + OFF, y1 + OFF, OUT_W, SHADOW3D + (255,))
    if not has(c + 1, r):  # right boundary
        vline(draw, y0 + OFF, y1 + OFF, x1 + OFF, OUT_W, SHADOW3D + (255,))

# --- 4. bright perimeter outline -------------------------------------------
for (c, r) in cells:
    x0, y0, x1, y1 = box(c, r)
    if not has(c, r - 1):
        hline(draw, x0, x1, y0, OUT_W, BRIGHT + (255,))
    if not has(c, r + 1):
        hline(draw, x0, x1, y1, OUT_W, BRIGHT + (255,))
    if not has(c - 1, r):
        vline(draw, y0, y1, x0, OUT_W, BRIGHT + (255,))
    if not has(c + 1, r):
        vline(draw, y0, y1, x1, OUT_W, BRIGHT + (255,))

# --- tagline (letter-spaced, centered) -------------------------------------
tx = (W - tag_w) / 2
ty = gy + grid_h + TAG_GAP
for ch, cw in zip(TAGLINE, char_w):
    draw.text((tx, ty), ch, font=font, fill=TAG_COL)
    tx += cw + TRACK

# --- downsample for antialiasing -------------------------------------------
final = img.resize((W // SS, H // SS), Image.LANCZOS)
final.save("docs/sx_logo.png")
print(f"wrote docs/sx_logo.png ({final.width}x{final.height})")
