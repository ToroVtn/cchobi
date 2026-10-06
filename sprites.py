"""The cat, in the style of "Replaced": no outlines, realistic proportions, a near-black
silhouette picked out by amber neon rim light, with dithered shadows.

Each frame is drawn in two passes:
  1. shapes (ellipses, tapered lines, triangles) paint *material* letters onto a 48x48 grid
  2. shade() turns materials into colours, adding rim light and dithering

Materials:  # fur   d far-side fur   w white fur   v white fur in shadow   e eye   i pupil
            n nose  m mouth   q tongue   p inside ear   l closed eyelid   k whisker
            s ground shadow   S sunbeam   . empty
Props:      b B h o G cushion (top, side, sheen, moon, stitching)   T t pot   L J leaves   Y U dry leaves   D soil
            X x spoon / bin   Q treat   H heart   A a Z watering can (body, shadow, shine)   R water
"""
import math

SIZE = 48
GROUND = 45          # y of the paws' centre
STRIDE, LIFT = 8, 3  # walk: how far a paw travels, how high it lifts
WALK_FRAMES = 8
HANG_FRAMES = 6

ACCENT = (235, 150, 70)                     # amber neon: rim light, also used for the speech bubble
ACCENT_DIM = (130, 80, 50)
FUR, FUR_DARK = (34, 30, 40), (20, 18, 26)
COOL = (45, 75, 90)                         # teal bounce light from the floor
WHITE, WHITE_DARK, WHITE_RIM = (220, 208, 190), (165, 155, 150), (255, 205, 150)
FLAT = {
    'e': (245, 150, 50), 'i': (15, 10, 10), 'n': (175, 105, 110), 'm': (85, 30, 40), 'q': (225, 120, 140),
    'p': (105, 62, 70), 'l': (170, 110, 60), 'k': (235, 230, 220, 150),
    's': (0, 0, 0, 100), 'S': (255, 205, 120, 60), '.': (0, 0, 0, 0),
    'b': (196, 222, 232), 'B': (150, 186, 205), 'h': (225, 240, 246), 'o': (232, 224, 118), 'G': (100, 172, 82),
    'T': (185, 95, 60), 't': (130, 62, 42), 'L': (110, 170, 85), 'J': (60, 115, 60), 'D': (70, 48, 36),
    'Y': (200, 165, 95), 'U': (135, 100, 55),
    'X': (175, 178, 190), 'x': (120, 122, 135), 'Q': (235, 160, 175), 'H': (240, 110, 130),
    'A': (95, 160, 150), 'a': (60, 110, 105), 'Z': (170, 215, 205), 'R': (150, 205, 255, 230),
}
BAYER = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]]  # ordered-dither pattern

# Points on the sprites (x, y in sprite pixels, facing right) that the app lines up with things.
HANG_GRAB = (24, 21)       # under your mouse pointer while she dangles
SLEEP_ANCHOR = (28, 37)    # rests on the cushion's seat
PERCH_ANCHOR = (24, 38)    # sits on a window's top edge
MUNCH_MOUTH = (44, 33)     # goes to a leaf on the plant
NIBBLE_MOUTH = (44, 40)    # goes to a leaf lying on the floor
RUB_CHEEK = (44, 26)       # presses against the plant
SCRATCH_WALL = (45, 26)    # x: the wall she's up against, scratching; y as RUB_CHEEK's, so she fits the same sides
FLOOR = (24, 47)           # stands on a floor: a window's top, or the bottom of the screen


class Canvas:
    def __init__(self, w=SIZE, h=SIZE):
        self.w, self.h = w, h
        self.g = [['.'] * w for _ in range(h)]
        # (angle, pivot x, pivot y, shift x, shift y[, scale x, scale y]): tilts, moves or squashes
        # what's drawn next, e.g. a head turning, or a body seen at an angle (foreshortened)
        self.tf = None

    def at(self, x, y):
        if not self.tf:
            return x, y
        a, px, py, dx, dy, sx, sy = (*self.tf, 1, 1)[:7]
        u, v = (x - px) * sx, (y - py) * sy
        return px + u * math.cos(a) - v * math.sin(a) + dx, py + u * math.sin(a) + v * math.cos(a) + dy

    def ellipse(self, cx, cy, rx, ry, ch, only=None, power=2):
        """only: paint just over these materials, e.g. white belly only where there's fur.
        power > 2 squares the ellipse off into a soft rounded rectangle (a squircle)."""
        cx, cy = self.at(cx, cy)
        if self.tf and len(self.tf) == 7:
            rx, ry = rx * self.tf[5], ry * self.tf[6]
        a = self.tf[0] if self.tf else 0
        ca, sa = math.cos(a), math.sin(a)
        bx, by = (max(rx, ry),) * 2 if a else (rx, ry)
        for y in range(max(0, int(cy - by)), min(self.h, int(cy + by) + 2)):
            for x in range(max(0, int(cx - bx)), min(self.w, int(cx + bx) + 2)):
                u, v = (x - cx) * ca + (y - cy) * sa, (y - cy) * ca - (x - cx) * sa  # undo the tilt
                if (abs(u / rx) ** power + abs(v / ry) ** power <= 1
                        and (only is None or self.g[y][x] in only)):
                    self.g[y][x] = ch

    def line(self, a, b, r0, r1, ch, only=None):
        """A thick line that tapers from radius r0 to r1 (a trail of discs)."""
        n = int(math.dist(a, b) * 2) + 1
        for k in range(n + 1):
            f = k / n
            r = r0 + (r1 - r0) * f
            self.ellipse(a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f, r, r, ch, only)

    def curve(self, p0, p1, p2, r0, r1, ch, only=None):
        """A thick tapered quadratic Bézier, for tails and backs."""
        pts = [tuple((1 - f) ** 2 * p0[i] + 2 * (1 - f) * f * p1[i] + f * f * p2[i] for i in (0, 1))
               for f in (k / 12 for k in range(13))]
        for k, (a, b) in enumerate(zip(pts, pts[1:])):
            self.line(a, b, r0 + (r1 - r0) * k / 12, r0 + (r1 - r0) * (k + 1) / 12, ch, only)

    def tri(self, a, b, c, ch):
        a, b, c = self.at(*a), self.at(*b), self.at(*c)

        def side(p, q, x, y):
            return (q[0] - p[0]) * (y - p[1]) - (q[1] - p[1]) * (x - p[0])
        for y in range(self.h):
            for x in range(self.w):
                s = side(a, b, x, y), side(b, c, x, y), side(c, a, x, y)
                if all(v >= 0 for v in s) or all(v <= 0 for v in s):
                    self.g[y][x] = ch

    def put(self, x, y, chars):
        for k, ch in enumerate(chars):
            px, py = (round(v) for v in self.at(x + k, y))
            if 0 <= px < self.w and 0 <= py < self.h:
                self.g[py][px] = ch


def shade(g):
    h, w = len(g), len(g[0])

    def empty(x, y):
        return not (0 <= x < w and 0 <= y < h) or g[y][x] in '.s'

    out = []
    for y, row in enumerate(g):
        line = []
        for x, ch in enumerate(row):
            # Light comes from above and in front, and fades towards the floor; the back edge gets
            # none, so that side of the silhouette melts into the dark like in the game.
            rim = (1 if empty(x, y - 1) else 0.6 if empty(x + 1, y) else 0) * (1.3 - 1.2 * y / SIZE)
            dither = BAYER[y % 4][x % 4] / 16
            if ch in '#d':
                c = (ACCENT if ch == '#' and rim > 0.6 else ACCENT_DIM if ch == '#' and rim > 0.25
                     else COOL if empty(x, y + 1)
                     else FUR_DARK if ch == 'd' or y / SIZE > dither * 0.5 + 0.45 else FUR)
            elif ch in 'wv':
                c = (WHITE_RIM if ch == 'w' and rim > 0.6
                     else WHITE_DARK if ch == 'v' or y / SIZE > dither * 0.15 + 0.85 else WHITE)
            else:
                c = FLAT[ch]
            line.append(c if len(c) == 4 else (*c, 255))
        out.append(line)
    return out


# ---- sitting, facing you ----
MOUTHS = {  # extra mouth shapes for the front view, as (x, y, pixels) patches
    'tongue': [(23, 19, 'qq'), (23, 20, 'q')],
    'lick_r': [(25, 19, 'q'), (26, 18, 'q')],                     # licking her lips, round to the right
    'lick_up': [(23, 18, 'qq'), (24, 17, 'q')],                   # ...up to her nose
    'lick_l': [(22, 19, 'q'), (21, 18, 'q')],                     # ...and round to the left
}


def front(eyes='open', mouth=None, paw=None, tilt=0, lean=0):
    """eyes: open / closed / happy.  mouth: None / meow / yawn_half / yawn / one of MOUTHS.
    paw: where her raised paw is while grooming; otherwise both paws are on the floor.
    tilt (radians) and lean (pixels) tip her head over to the right."""
    c = Canvas()
    legs = (28,) if paw else (20, 28)
    c.ellipse(24, 46.5, 17, 1.3, 's')
    c.curve((30, 45), (45, 47), (42, 33), 2.4, 1.5, '#')                    # tail curled round
    c.ellipse(24, 38, 13, 8.5, '#')                                         # haunches
    c.ellipse(24, 30, 8, 10, '#')                                           # chest
    for x in legs:
        c.line((x, 31), (x, 44), 2.2, 2, '#')                               # front legs
    c.ellipse(24, 31, 4.5, 9, 'w', only='#')                                # white bib
    c.ellipse(24, 42, 1.6, 4, 'd')                                          # shadow between the legs
    for x in legs:
        c.ellipse(x, 45, 2.6, 1.5, 'w')                                     # white gloves
    c.tf = (tilt, 24, 20, lean, 0) if tilt or lean else None                # the head and face tilt together
    front_head(c, eyes, mouth)
    c.tf = None
    if paw:                                                                 # raised paw, over the face
        c.line((19, 30), paw, 2.2, 1.9, '#')
        c.ellipse(paw[0], paw[1], 2.3, 1.9, 'w')
    return c.g


def front_head(c, eyes='open', mouth=None):
    """Her face, looking at you, centred on (24, 15). Move or squash it with c.tf."""
    for s in (1, -1):                                                       # ears, left then mirrored
        c.tri((24 - 7.5 * s, 12), (24 - 6.5 * s, 3), (24 - 1 * s, 9), '#')
        c.tri((24 - 6 * s, 10), (24 - 5.7 * s, 5.5), (24 - 2.5 * s, 9), 'p')
    c.ellipse(24, 15, 8, 6.5, '#')                                          # head
    c.ellipse(24, 17, 9, 4.5, '#')                                          # cheeks
    for x, y in ((24, 11), (25, 12), (23, 13), (24, 14), (22, 15)):        # white speckles on her nose
        c.put(x, y, 'w')
    c.put(22, 16, 'nnnn'), c.put(23, 17, 'nn'), c.put(23, 19, 'mm')
    if eyes == 'open':
        c.put(19, 13, 'eie'), c.put(19, 14, 'eie'), c.put(27, 13, 'eie'), c.put(27, 14, 'eie')
    elif eyes == 'closed':
        c.put(19, 14, 'lll'), c.put(27, 14, 'lll')
    else:                                                                   # happy ^ ^
        for x in (19, 27):
            c.put(x + 1, 13, 'l'), c.put(x, 14, 'l'), c.put(x + 2, 14, 'l')
    c.put(10, 18, 'kkkkk'), c.put(11, 20, 'kkkk'), c.put(33, 18, 'kkkkk'), c.put(33, 20, 'kkkk')  # whiskers
    if mouth == 'meow':
        c.ellipse(24, 19.5, 1.8, 1.6, 'm')
    elif mouth == 'yawn_half':
        c.ellipse(24, 19.8, 1.8, 1.5, 'm')
    elif mouth == 'yawn':                                                   # wide open: fangs and tongue
        c.ellipse(24, 20.3, 2.8, 2.8, 'm'), c.put(23, 22, 'qq'), c.put(22, 18, 'w'), c.put(25, 18, 'w')
    for x, y, px in MOUTHS.get(mouth, []):
        c.put(x, y, px)


def back_head(c):
    """The back of her head: ears and fluff, no face. Centred on (24, 15) like front_head."""
    for s in (1, -1):
        c.tri((24 - 7.5 * s, 12), (24 - 6.5 * s, 3), (24 - 1 * s, 9), '#')
    c.ellipse(24, 15, 8, 6.5, '#'), c.ellipse(24, 17, 9, 4.5, '#')
    c.put(12, 18, 'kk'), c.put(35, 18, 'kk')                                # whisker tips poking out


# ---- side-view head, shared by the resting poses ----
def side_head(c, x, y, eyes='open', mouth=None):
    c.tri((x - 4.5, y - 3), (x - 3.5, y - 9.5), (x - 1, y - 4.5), 'd')         # far ear
    c.ellipse(x, y, 6, 5.2, '#')
    c.ellipse(x + 4.5, y + 2, 2.8, 2.2, '#')                                     # snout
    c.tri((x - 1.5, y - 4.5), (x + 1.5, y - 10.5), (x + 3.5, y - 4), '#')        # near ear
    c.tri((x - .2, y - 5), (x + 1.3, y - 8.5), (x + 2.5, y - 5), 'p')
    if eyes == 'open':
        c.put(x + 1, y - 1, 'eie'), c.put(x + 2, y - 2, 'e')
    else:
        c.put(x + 1, y - 1, 'lll')
    c.put(x + 4, y - 2, 'w'), c.put(x + 5, y - 1, 'w')                          # speckles
    c.put(x + 7, y + 1, 'n')
    if mouth == 'open':
        c.put(x + 5, y + 3, 'mm')
    elif mouth == 'tongue':
        c.put(x + 6, y + 3, 'q'), c.put(x + 6, y + 4, 'q'), c.put(x + 7, y + 5, 'q')
    else:
        c.put(x + 7, y + 3, 'kkk')                                               # whiskers


# ---- walking, facing right ----
def knee(hip, foot, upper, lower):
    """Two-bone leg: where the joint goes so both bones reach the foot (bent backwards)."""
    dx, dy = foot[0] - hip[0], foot[1] - hip[1]
    dist = math.hypot(dx, dy)
    d = min(dist, upper + lower - 0.01)
    ux, uy = dx / dist, dy / dist
    a = (upper ** 2 - lower ** 2 + d ** 2) / (2 * d)
    h = math.sqrt(max(0, upper ** 2 - a ** 2))
    bx, by = hip[0] + ux * a, hip[1] + uy * a
    return min((bx - uy * h, by + ux * h), (bx + uy * h, by - ux * h))  # the one further back


def leg(c, hip, t, upper, lower, thigh, fur, sock):
    if t < 0.6:   # paw on the ground, sliding back as the body moves over it
        foot = (hip[0] + STRIDE * (0.5 - t / 0.6), GROUND)
    else:         # paw lifted, swinging forward
        s = (t - 0.6) / 0.4
        foot = (hip[0] + STRIDE * (s - 0.5), GROUND - LIFT * math.sin(math.pi * s))
    k = knee(hip, foot, upper, lower)
    c.line(hip, k, thigh, 1.3, fur)
    c.line(k, foot, 1.3, 1.1, fur)
    c.line((k[0] + (foot[0] - k[0]) * .55, k[1] + (foot[1] - k[1]) * .55), foot, 1.2, 1.2, sock)
    c.ellipse(foot[0] + 0.8, foot[1], 1.8, 1.1, sock)


def standing_leg(c, hip, foot_x, fur, sock):
    c.line(hip, (foot_x, GROUND), 2, 1.2, fur)
    c.line((foot_x, GROUND - 3), (foot_x, GROUND), 1.2, 1.2, sock)
    c.ellipse(foot_x + .8, GROUND, 1.8, 1.1, sock)


def side(t, view='side'):
    """Walking, facing right. view: 'side' (in profile), 'toward' (coming at you diagonally: the body
    foreshortened, her face turned to you) or 'away' (going off diagonally: the back of her head,
    her tail nearest you)."""
    c = Canvas()
    b = 0.6 * math.sin(4 * math.pi * t)            # body bob, twice per stride
    hind, fore = (13, 30 + b), (30, 31 + b)
    sway = 1.5 * math.sin(2 * math.pi * t)
    c.ellipse(23, 46.5, 15 if view == 'side' else 11, 1.2, 's')
    if view != 'side':
        c.tf = (0, 24, 30, 0, 0, .62, 1)            # seen at an angle: squashed front to back

    def tail():
        c.curve((11, 25 + b), (1, 25 + b), (4 + sway, 11 + b), 2.2, 1.2, '#')

    leg(c, hind, (t + 0.5) % 1, 9, 8, 2.5, 'd', 'v')   # far legs first, so the body covers them
    leg(c, fore, (t + 0.75) % 1, 8, 7, 1.8, 'd', 'v')
    if view != 'away':
        tail()
    c.ellipse(21, 28 + b, 12, 6, '#')                  # body
    c.ellipse(13, 28 + b, 6.5, 6.5, '#')               # haunch
    c.ellipse(30, 28 + b, 5, 6, '#')                   # shoulders
    c.line((35, 21 + b), (30, 26 + b), 4.5, 5.5, '#')  # neck
    if view == 'side':
        c.tri((32, 16 + b), (33, 9 + b), (36, 14 + b), 'd')                    # far ear
        c.ellipse(37, 19 + b, 6.5, 5.5, '#')           # head
        c.ellipse(42, 21 + b, 3.2, 2.5, '#')           # snout
        c.tri((36, 15 + b), (38, 8 + b), (41, 15 + b), '#')                    # near ear
        c.tri((37.3, 14 + b), (38, 10 + b), (39.7, 14 + b), 'p')
    c.ellipse(35.5, 28 + b, 2.5, 5, 'w', only='#')     # chest, on the front edge
    c.ellipse(22, 34 + b, 9, 2.2, 'w', only='#')       # belly
    leg(c, hind, t, 9, 8, 2.5, '#', 'w')
    leg(c, fore, (t + 0.25) % 1, 8, 7, 1.8, '#', 'w')
    if view == 'away':
        tail()                                         # her rear is the nearest part now
    c.tf = None
    if view == 'side':
        y = round(18 + b)
        c.put(39, y, 'eie'), c.put(40, y - 1, 'e')
        c.put(42, y - 1, 'w'), c.put(43, y, 'w')       # white speckles above the nose
        c.put(45, y + 2, 'n'), c.put(43, y + 4, 'm'), c.put(44, y + 3, 'kkkk')
    elif view == 'toward':                             # her face, turned to you, at the front end
        c.tf = (0, 24, 15, 9.5, 4 + b, .8, .8)
        front_head(c)
        c.tf = None
    else:                                              # the back of her head, a bit further off
        c.tf = (0, 24, 15, 8, 1.5 + b, .72, .72)
        back_head(c)
        c.tf = None
    return c.g


def step(c, x, ground, t, fur, sock, top):
    """A leg seen from the front or behind: it lifts as it swings through."""
    lift = LIFT * math.sin(math.pi * (t - .6) / .4) if t >= .6 else 0
    foot = ground - lift
    c.line((x, top), (x, foot), 2.1, 1.8, fur)
    c.line((x, foot - 3), (x, foot), 1.8, 1.8, sock)
    c.ellipse(x, foot + .3, 2.1, 1.2, sock)


def walk_toward(t):
    """Walking straight at you."""
    c = Canvas()
    b, sway = 0.6 * math.sin(4 * math.pi * t), 1.5 * math.sin(2 * math.pi * t)
    c.ellipse(24, 46.5, 11, 1.3, 's')
    c.curve((27, 27 + b), (36, 24 + b), (37 + sway, 11 + b), 2, 1.2, '#')       # tail, up behind her
    for x, phase in ((19, .5), (29, 0)):                                         # hind legs, further back
        step(c, x, 42, (t + phase) % 1, 'd', 'v', top=30 + b)
    c.ellipse(24, 31 + b, 8, 7, '#')                                              # body, behind her chest
    c.ellipse(24, 28 + b, 6.5, 8, '#')                                            # chest
    c.ellipse(24, 29 + b, 3.5, 6.5, 'w', only='#')                                # bib
    for x, phase in ((20.5, 0), (27.5, .5)):                                      # front legs
        step(c, x, GROUND, (t + phase) % 1, '#', 'w', top=32 + b)
    c.tf = (0, 24, 15, 0, 1 + b)
    front_head(c)
    c.tf = None
    return c.g


def walk_away(t):
    """Walking straight away from you: her back, her bottom and her tail up."""
    c = Canvas()
    b, sway = 0.6 * math.sin(4 * math.pi * t), 1.5 * math.sin(2 * math.pi * t)
    c.ellipse(24, 46.5, 11, 1.3, 's')
    for x, phase in ((20.5, .5), (27.5, 0)):                                      # front legs, further off
        step(c, x, 42, (t + phase) % 1, 'd', 'v', top=30 + b)
    c.tf = (0, 24, 15, 0, b)
    back_head(c)                                                                  # beyond her back
    c.tf = None
    c.ellipse(24, 29 + b, 8, 7.5, '#')                                            # back
    c.ellipse(19.5, 35 + b, 5, 5.5, '#'), c.ellipse(28.5, 35 + b, 5, 5.5, '#')    # haunches
    for x, phase in ((19.5, 0), (28.5, .5)):                                      # hind legs
        step(c, x, GROUND, (t + phase) % 1, '#', 'w', top=37 + b)
    c.curve((25, 32 + b), (34 + sway, 27 + b), (35 + 1.5 * sway, 12 + b), 2.2, 1.3, '#')  # tail up, to one side
    return c.g


# ---- resting poses ----
def loaf(blink=False, dy=0, tail_swing=None):
    """A bread-shaped lump, paws tucked away. With tail_swing she's perched on an edge,
    shifted up by -dy, her tail hanging down over it."""
    c = Canvas()
    if tail_swing is None:
        c.ellipse(24, 46.5 + dy, 14, 1.2, 's')
    else:
        c.curve((14, 42 + dy), (11, 47 + dy), (12 + tail_swing, 47.5), 2, 1.3, '#')
    c.ellipse(24, 37 + dy, 12, 7.5, '#', power=2.6)                            # body
    if tail_swing is None:
        c.curve((13, 41 + dy), (14, 46 + dy), (27, 45.5 + dy), 2, 1.3, '#')    # tail wrapped round
    c.ellipse(37, 37 + dy, 2.5, 4.5, 'w', only='#')                            # chest
    c.ellipse(38, 43.5 + dy, 2, 1.2, 'w')                                       # a tucked glove
    side_head(c, 34, 28 + dy, eyes='closed' if blink else 'open')
    return c.g


def sphinx(flick):
    """Lying down, front legs stretched out in front, head up."""
    c = Canvas()
    c.ellipse(24, 46.5, 18, 1.2, 's')
    c.line((31, 41.5), (41, 43.5), 1.6, 1.4, 'd'), c.ellipse(41.8, 43.8, 1.9, 1.1, 'v')   # far front leg
    c.curve((9, 41), (2, 45), (7 + flick, 46), 2, 1.3, '#')                                 # tail
    c.ellipse(22, 38, 13, 6, '#')                                                           # body
    c.ellipse(13, 38, 6.5, 6.5, '#')                                                        # haunch
    c.ellipse(19, 44.6, 2.5, 1, 'w')                                                        # back paw
    c.line((32, 36), (35, 31), 5, 5, '#')                                                   # neck
    c.ellipse(34, 37, 3, 4, 'w', only='#')                                                  # chest
    c.ellipse(24, 43, 7, 1.5, 'w', only='#')                                                # belly
    c.line((32, 42), (43, 44), 1.8, 1.6, '#'), c.ellipse(43.8, 44.3, 2, 1.2, 'w')           # near front leg
    side_head(c, 36, 27)
    return c.g


def sunbathe(breath):
    """Stretched out on her side in a patch of sun, eyes shut."""
    c = Canvas()
    c.tri((6, 38), (46, 38), (42, 47), 'S'), c.tri((6, 38), (42, 47), (1, 47), 'S')    # the sunbeam
    c.curve((9, 41), (3, 42), (1, 46), 2, 1.2, '#')                                     # tail
    for hip, foot in (((14, 42), (5, 45.5)), ((33, 42), (44, 45.5))):                    # far legs
        c.line(hip, foot, 1.6, 1.2, 'd'), c.ellipse(foot[0], foot[1], 1.8, 1, 'v')
    c.ellipse(23, 40 - breath, 14, 5 + breath, '#')                                      # body
    c.ellipse(26, 42.5, 9, 2.2, 'w', only='#')                                           # belly to the sun
    for hip, foot in (((15, 43), (7, 46)), ((32, 43), (42, 46))):                        # near legs
        c.line(hip, foot, 1.8, 1.3, '#'), c.ellipse(foot[0], foot[1], 2, 1.1, 'w')
    side_head(c, 38, 37, eyes='closed')
    return c.g


def rub(k):
    """Pressing her cheek into something in front of her, eyes shut, tail up."""
    c = Canvas()
    push = (0, 1, 2, 1)[k]
    c.ellipse(23, 46.5, 15, 1.2, 's')
    standing_leg(c, (15, 30), 16, 'd', 'v'), standing_leg(c, (31, 31), 32 + push * .5, 'd', 'v')
    c.curve((10, 25), (3, 17), (7, 6), 2.2, 1.3, '#'), c.curve((7, 6), (9, 3), (10, 5), 1.3, 1, '#')
    c.ellipse(21 + push * .4, 29, 12, 6, '#')                                   # body
    c.ellipse(13, 28, 6.5, 6.5, '#')
    c.ellipse(30 + push * .6, 30, 5, 6, '#')                                    # shoulders
    c.line((31 + push * .6, 28), (37 + push, 26), 4.5, 4.5, '#')                # neck, stretched forward
    c.ellipse(35.5 + push, 30, 2.5, 4.5, 'w', only='#')
    c.ellipse(22, 34, 9, 2, 'w', only='#')
    standing_leg(c, (13, 30), 13, '#', 'w'), standing_leg(c, (29, 31), 30 + push * .5, '#', 'w')
    side_head(c, 38 + push, 25, eyes='closed')
    return c.g


def crouch(k, treat=False, low=False):
    """Head down at something: munching a plant's leaves, a dry leaf on the floor (low), or
    licking a treat off a spoon."""
    c = Canvas()
    bob = (0, 1, 0, 1)[k]
    head_y = (36 if treat or low else 30) + bob
    c.ellipse(22, 46.5, 15, 1.2, 's')
    if treat:
        c.line((47.5, 41.5), (45, 44), .6, .6, 'X')                            # spoon handle
        c.ellipse(44.5, 44.4, 2.6, 1.1, 'X'), c.ellipse(44.3, 43.6, 1.8, .9, 'Q')
    standing_leg(c, (15, 30), 16, 'd', 'v'), standing_leg(c, (31, 32), 32, 'd', 'v')
    c.curve((10, 27), (3, 31), (5, 41), 2, 1.2, '#')                            # tail, relaxed
    c.ellipse(21, 29, 12, 6, '#'), c.ellipse(13, 28, 6.5, 6.5, '#'), c.ellipse(30, 31, 5, 6, '#')
    c.line((31, 30), (37, head_y - 1), 4.5, 4, '#')                             # neck, reaching down
    c.ellipse(22, 34, 9, 2, 'w', only='#')
    standing_leg(c, (13, 30), 13, '#', 'w'), standing_leg(c, (29, 32), 30, '#', 'w')
    mouth = ('tongue' if k % 2 == 0 else None) if treat else ('open' if k % 2 else None)
    side_head(c, 38, head_y, eyes='closed' if treat else 'open', mouth=mouth)
    return c.g


# ---- stretching: first the front (a bow), then a back leg ----
def stretch_front(s, rake=(0, 0), eyes='closed'):
    """s 0..1: from standing to a full bow: front legs reaching out along the floor, chest down,
    bottom and tail up. rake: how far the far and near front paws have pulled back (scratching)."""
    c = Canvas()
    reach = 34 + 10 * s                                        # where the front paws get to
    c.ellipse(24, 46.5, 17, 1.2, 's')
    c.line((16, 27 - 3 * s), (16.5, GROUND), 2, 1.2, 'd'), c.ellipse(17.3, GROUND, 1.8, 1.1, 'v')       # far hind leg
    far = reach - 2 - rake[0]
    c.line((30, 32 + 5 * s), (far, 44.5), 1.8, 1.3, 'd'), c.ellipse(far + .5, 44.6, 1.9, 1.1, 'v')
    c.curve((11, 25 - 4 * s), (5, 16 - 4 * s), (8, 5), 2.2, 1.3, '#')                                  # tail up
    c.tf = (.15 + .4 * s, 22, 30, 0, 0)                        # the body tips forward, chest going down
    c.ellipse(22, 30, 12.5, 5.6, '#'), c.ellipse(11, 30, 6.5, 6.2, '#'), c.ellipse(31, 30, 5, 5.5, '#')
    c.ellipse(22, 34.5, 8, 1.8, 'w', only='#')                 # belly
    c.tf = None
    c.line((14, 28 - 3 * s), (14.5, GROUND), 2.2, 1.3, '#')                                            # near hind leg
    c.line((14.5, GROUND - 3), (14.5, GROUND), 1.2, 1.2, 'w'), c.ellipse(15.3, GROUND, 1.8, 1.1, 'w')
    near = reach - rake[1]
    c.line((31, 34 + 5 * s), (near, 44.5), 2, 1.4, '#')                                                # near front leg
    c.ellipse(near + .5, 44.6, 2.2, 1.2, 'w')
    for paw, back in ((far, rake[0]), (near, rake[1])):
        if back:                                               # claws out, raking
            c.put(paw + 2.5, 45, 'k'), c.put(paw + 2, 44, 'k')
    side_head(c, 36 + 1.5 * s, 25 + 11 * s, eyes=eyes)         # head goes down with the chest
    return c.g


def scratch_wall(k):
    """Up on her hind legs against a wall in front of her, front paws raking down it in turn."""
    c = Canvas()
    far, near = (0, 2, 4, 2)[k], (4, 2, 0, 2)[k]              # how far each paw has pulled down
    c.ellipse(21, 46.5, 10, 1.2, 's')
    standing_leg(c, (23, 39), 24, 'd', 'v')                                                  # far hind leg
    c.line((30, 24), (42.5, 4 + far), 1.8, 1.3, 'd'), c.ellipse(43.3, 4 + far, 1.2, 1.9, 'v')  # far front leg
    c.curve((17, 40), (5, 45), (4, 35), 2.2, 1.3, '#')                                       # tail, on the floor
    c.ellipse(20, 38, 6.5, 6.5, '#')                                                         # haunch
    c.line((22, 36), (28, 24), 6, 5, '#')                                                    # body, upright
    c.ellipse(29, 24, 4.5, 5, '#')                                                           # shoulders
    c.line((26, 35), (31, 27), 1.3, 1.5, 'w', only='#')                                      # belly and chest
    standing_leg(c, (19, 40), 20, '#', 'w')                                                  # near hind leg
    c.line((31, 25), (42.5, 8 + near), 2, 1.5, '#'), c.ellipse(43.3, 8 + near, 1.3, 2.1, 'w')  # near front leg
    side_head(c, 29, 17)                                                                     # looking up the wall
    for y, back in ((4 + far, far), (8 + near, near)):
        if back:                                                                             # claws out
            c.put(44, y - 1, 'k'), c.put(44, y + 1, 'k')
    return c.g


def stretch_back(s):
    """s 0..1: rocked forward onto straight front legs, one back leg pushed straight out behind."""
    c = Canvas()
    c.ellipse(23, 46.5, 17, 1.2, 's')
    standing_leg(c, (15, 31), 15, 'd', 'v'), standing_leg(c, (31, 30), 32, 'd', 'v')                  # far legs
    c.curve((11, 27), (4, 19), (7, 7), 2.2, 1.3, '#')                                                  # tail up
    c.tf = (-.12, 22, 29, 1.5 * s, 0)                          # body leaning forward and up a little
    c.ellipse(22, 29, 12, 5.8, '#'), c.ellipse(13, 30, 6.5, 6.2, '#'), c.ellipse(31, 28, 5, 6, '#')
    c.ellipse(22, 33.5, 8, 1.8, 'w', only='#'), c.ellipse(35.5, 28, 2.5, 4.5, 'w', only='#')
    c.tf = None
    foot = (3 - 2 * s, 42 - 4 * s)                             # the stretched leg, straight out behind
    c.line((13, 32), foot, 2.2, 1.2, '#'), c.line((foot[0] + 2.5, foot[1] - .4), foot, 1.2, 1.2, 'w')
    c.ellipse(foot[0] - .6, foot[1], 1.8, 1.1, 'w')
    standing_leg(c, (30, 31), 32.5, '#', 'w')                                                          # near front leg
    side_head(c, 39 + 1.5 * s, 20 - s, eyes='closed')          # chin up, eyes shut: bliss
    return c.g


# ---- grabbed by the belly, dangling ----
def hang(t):
    """Draped over your hand: back in an upside-down U, legs and tail hanging limp."""
    c = Canvas()
    s = math.sin(2 * math.pi * t)

    def limb(hip, swing, fur, sock):
        foot = (hip[0] + swing, 45)
        c.line(hip, foot, 2, 1.3, fur)
        c.line((hip[0] + swing * .85, 42), foot, 1.4, 1.4, sock)

    limb((31, 31), .8 * s, 'd', 'v'), limb((17, 31), -.8 * s, 'd', 'v')          # far legs
    c.curve((14, 27), (8, 35), (10 + 2 * s, 45), 2, 1.2, '#')                     # tail
    c.curve((34, 31), (24, 1), (14, 31), 5.5, 5.5, '#')                           # back
    c.tri((35, 23), (35.5, 16.5), (38.5, 21.5), 'd')                              # far ear
    c.ellipse(38.5, 26, 5.5, 5, '#'), c.ellipse(42, 28.2, 2.4, 1.9, '#')          # head hanging forward
    c.tri((37.5, 21.5), (41, 15.5), (43, 22.5), '#'), c.tri((39, 21), (40.8, 17.5), (41.8, 21.5), 'p')
    c.ellipse(35.5, 33, 2.2, 3, 'w', only='#')                                    # chest
    limb((33, 32), -s, '#', 'w'), limb((15, 32), s, '#', 'w')                     # near legs
    c.put(39, 25, 'eie'), c.put(40, 24, 'e'), c.put(42, 25, 'w'), c.put(44, 27, 'n')
    c.put(44, 29, 'kkk')
    return c.g


# ---- curled up asleep ----
def sleep(breath):
    c = Canvas()
    c.ellipse(29, 30 - breath, 10, 6 + breath, '#')                                          # curled body
    c.ellipse(20, 32, 5.5, 4.5, '#')                                                         # head on her paws
    c.tri((15, 30), (14, 24), (19, 28.5), '#'), c.tri((19.5, 28), (22, 23.5), (24, 29), '#')  # ears
    c.tri((15.5, 29), (15, 26), (17.5, 28.5), 'p'), c.tri((20.5, 28), (22, 25.5), (23, 28.5), 'p')
    c.ellipse(25, 36, 2.2, 1.2, 'w')                                                         # a glove peeking out
    c.curve((38, 31), (42, 39), (25, 37.5), 2.2, 1.4, '#')                                   # tail wrapped round
    c.put(16, 32, 'll'), c.put(15, 33, 'n'), c.put(17, 30, 'w')
    return c.g


# ---- her cushion: a puffy rounded pillow. squish 0 = resting, 1 = flattened, < 0 = puffed up ----
CUSHION_W, CUSHION_H = 56, 24
CUSHION_SQUISH = [0, .35, .45, .75, 1, -.15]  # frames: rest, sleeping (2 breaths), plop (2), rebound


def cushion_shape(squish):
    """(rx, ry, cy, top_y, top_ry): flatter and wider the more she squishes it."""
    rx, ry = 25 * (1 + .06 * squish), 8.5 * (1 - .3 * squish)
    cy = 22 - ry                                                       # squashes down, bottom stays put
    return rx, ry, cy, cy - 2.6 * (1 - .3 * squish), 7 * (1 - .35 * squish)


def cushion_seat(squish):
    """Where on the cushion she lies (right on the moon, like the real one); it sinks as it squishes."""
    return 28, cushion_shape(squish)[3] + 2


def cushion(squish):
    """Her square, puffy, pale blue pillow with the moon in the middle, like the real one."""
    c = Canvas(CUSHION_W, CUSHION_H)
    rx, ry, cy, top_y, top_ry = cushion_shape(squish)
    flat = 1 - .3 * squish
    c.ellipse(28, 22.6, rx + 1, 1.3, 's')
    c.ellipse(28, cy, rx, ry, 'B', power=3.5)                          # puffy sides, soft square corners
    c.ellipse(28, top_y, rx - 1.2, top_ry, 'b', power=3.5)             # top
    c.ellipse(19, top_y - top_ry * .5, 9, 1.3, 'h', only='b')          # sheen
    # the moon: a yellow crescent with green stitching, in the middle
    mx, my = 28, top_y + .5
    c.ellipse(mx, my, 8, 3.8 * flat, 'G', only='bh'), c.ellipse(mx, my, 7, 2.9 * flat, 'o', only='G')
    c.ellipse(mx + 3, my - 1.6 * flat, 7.5, 3.3 * flat, 'G', only='oG')
    c.ellipse(mx + 3, my - 1.6 * flat, 6.5, 2.5 * flat, 'b', only='G')
    return c.g


# ---- her plant: a jade plant in a terracotta pot. sway -1..1 shakes the leaves ----
PLANT_W, PLANT_H = 24, 32
PLANT_SWAY = [0, -1, 1]
PLANT_BITE = (4, 19)       # the low left leaf (the first to dry out), at her mouth's height beside it
PLANT_SOIL = (6, 20)       # where she reaches into the pot for a leaf lying in it
PLANT_CHEEK = (6, 12)      # where she rubs, on the left
PLANT_LEAVES = [(12, 8, 3, 4.5), (7, 11, 3.5, 2.5), (17, 10, 3.5, 2.5), (12, 14, 3, 3.5),
                (5, 16, 3.2, 2.2), (19, 15, 3.2, 2.2), (9, 18, 3, 2), (15, 18, 3, 2)]
DRY_ORDER = [4, 5, 6, 7, 1, 2, 3, 0]   # which leaves go brown first: low and outside, the top one last
POT_LEAVES = [(8.5, 20.2, .4), (15, 20.4, -.3), (11.5, 19.7, .9), (13, 20.9, -.9)]  # dry leaves in the pot
POT_MOUTH = (6, 17, 18, 23)  # x0, y0, x1, y1: a leaf falling through here lands in the pot


def plant(sway, slots=None, pot=0):
    """slots: 'green' / 'dry' / 'gone' for each of PLANT_LEAVES. pot: dry leaves lying in the pot."""
    slots = slots or ['green'] * len(PLANT_LEAVES)
    c = Canvas(PLANT_W, PLANT_H)
    c.ellipse(12, 31.3, 9, 1, 's')
    c.tri((5, 22), (19, 22), (17, 31), 'T'), c.tri((5, 22), (17, 31), (7, 31), 'T')    # pot
    c.tri((5, 22), (8.5, 22), (7, 31), 't'), c.tri((5, 22), (7, 31), (7.5, 31), 't')
    c.ellipse(12, 21, 8.5, 1.8, 'T', power=3), c.ellipse(12, 20.6, 7, .9, 'D')          # rim, soil
    for k, (x, y, rx, ry) in enumerate(PLANT_LEAVES):
        if slots[k] == 'gone':
            continue
        dead = slots[k] == 'dry'
        if dead:                                                                        # droops and shrivels
            y, ry = y + 1.5, ry * .75
        x += sway * (20 - y) / 10                                                       # tops sway most
        c.line((12, 20), (x, y), .6, .6, 'U' if dead else 'J')
        c.ellipse(x, y, rx, ry, 'U' if dead else 'J')
        c.ellipse(x + .6, y - .6, rx - .8, ry - .8, 'Y' if dead else 'L')
    for x, y, a in POT_LEAVES[:pot]:                                                    # dry leaves lying in
        c.tf = (a, x, y, 0, 0)                                                          # the pot, on top
        c.ellipse(x, y, 2.8, 1.1, 'U'), c.ellipse(x + .3, y - .3, 1.9, .6, 'Y')
        c.tf = None
    return c.g


# ---- a dry leaf that has fallen off: lies about, gets dragged, eaten or thrown away ----
LEAF_SIZE = 10


def leaf(angle):
    c = Canvas(LEAF_SIZE, LEAF_SIZE)
    c.tf = (angle, 5, 5, 0, 0)
    c.ellipse(5, 5, 3.4, 1.6, 'U'), c.ellipse(5.3, 4.7, 2.4, .9, 'Y'), c.line((1, 5.4), (2.2, 5.2), .5, .5, 'U')
    c.tf = None
    return c.g


def leaf_frames():
    """Lying flat first, then turned at a few angles for tumbling through the air."""
    return [shade(leaf(a)) for a in (0, .8, 1.6, 2.4)]


# ---- a bin for throwing dry leaves away ----
BIN_W, BIN_H = 18, 22
BIN_MOUTH = (9, 6)         # where leaves go in


def trash_bin(lid_open):
    c = Canvas(BIN_W, BIN_H)
    c.ellipse(9, 21.3, 7.5, .9, 's')
    c.tri((3, 7), (15, 7), (14, 21), 'X'), c.tri((3, 7), (14, 21), (4, 21), 'X')        # the bin
    c.tri((3, 7), (5.5, 7), (4, 21), 'x'), c.tri((3, 7), (4, 21), (4.6, 21), 'x')       # its shadowed side
    for x in (7, 9.5, 12):
        c.line((x, 9), (x - .3, 19), .4, .4, 'x')                                       # ridges
    c.ellipse(9, 7, 6.5, 1.3, 'i')                                                      # the dark opening
    if lid_open:                                                                        # flipped up behind
        c.tf = (-1.2, 2.5, 6, 0, 0)
        c.ellipse(9, 6, 7, 1.4, 'x'), c.ellipse(9, 5.4, 2, .7, 'X')
    else:
        c.ellipse(9, 6, 7, 1.4, 'X'), c.ellipse(9, 4.8, 2, .8, 'x')                     # lid and its knob
    c.tf = None
    return c.g


def bin_frames():
    return [shade(trash_bin(False)), shade(trash_bin(True))]


# ---- a watering can for the plant: tips forward and pours ----
CAN_W, CAN_H = 40, 40
CAN_POUR = (33.5, 39)      # where its water lands: lined up with the top of the plant


def can(tilt, drops=None):
    """tilt in radians (forward = spout down); drops: which step of the falling-water loop, or None."""
    c = Canvas(CAN_W, CAN_H)
    c.tf = (tilt, 14, 22, 0, 0)                                  # tips around its middle
    c.curve((10, 17), (14, 9), (19, 17), .8, .8, 'a')            # handle over the top
    c.ellipse(14, 22, 7, 6, 'A', power=2.6)                      # body
    c.ellipse(14, 26, 6.5, 2.2, 'a', only='A')                   # its lower half in shadow
    c.ellipse(12, 19, 3, 1, 'Z', only='A')                       # shine
    c.line((19, 22), (29, 15), 1.3, .8, 'A')                     # spout
    c.ellipse(30, 14.5, 1.6, 1.2, 'a')                           # the sprinkler rose on the end
    tip = c.at(31, 14.5)
    c.tf = None
    if drops is not None:                                        # water falling from the spout
        for j in range(3):
            y = tip[1] + 2 + (drops * 5 + j * 7) % 20
            c.ellipse(tip[0] + 1 + (y - tip[1]) * .08, y, .8, 1.3, 'R')
    return c.g


def can_frames():
    """Upright, tipping, then pouring (4 steps of falling drops)."""
    return [shade(can(t, d)) for t, d in ((0, None), (.3, None), (.6, 0), (.6, 1), (.6, 2), (.6, 3))]


HEART = """
.HH.HH.
HHHHHHH
HHHHHHH
.HHHHH.
..HHH..
...H...
""".split()


def frames():
    """{name: rows of RGBA tuples}, all facing right."""
    out = {'sit': front(), 'meow': front(mouth='meow'), 'pet': front(eyes='happy'),
           'yawn0': front(eyes='closed', mouth='yawn_half'), 'yawn1': front(eyes='closed', mouth='yawn')}
    for i, mouth in enumerate(('lick_r', 'lick_up', 'lick_l', None)):
        out[f'smack{i}'] = front(eyes='closed', mouth=mouth)
    for i, (tilt, lean) in enumerate(((.15, 1), (.25, 2), (.32, 3), (.25, 2))):
        out[f'rubf{i}'] = front(eyes='closed', tilt=tilt, lean=lean)  # cheek pushed into your pointer
    for i, (paw, mouth) in enumerate((((21.5, 21.5), 'tongue'), ((21.5, 20.5), None),
                                      ((20.5, 15.5), None), ((21, 18), None))):
        out[f'groom{i}'] = front(eyes='closed', mouth=mouth, paw=paw)
    for i in range(WALK_FRAMES):  # walking in 8 directions: these plus their mirror images
        t = i / WALK_FRAMES
        out[f'walk{i}'] = side(t)                     # right
        out[f'walkqf{i}'] = side(t, 'toward')         # down-right
        out[f'walkqb{i}'] = side(t, 'away')           # up-right
        out[f'walkf{i}'] = walk_toward(t)             # down
        out[f'walkb{i}'] = walk_away(t)               # up
    out.update({f'hang{i}': hang(i / HANG_FRAMES) for i in range(HANG_FRAMES)})
    out.update({f'stretchf{i}': stretch_front((i + 1) / 2) for i in range(2)})   # halfway, then all the way
    out.update({f'stretchb{i}': stretch_back((i + 1) / 2) for i in range(2)})
    out.update({f'scratchw{i}': scratch_wall(i) for i in range(4)})
    out.update({f'scratchf{i}': stretch_front(.8, rake, 'open')
                for i, rake in enumerate(((0, 4), (2, 2), (4, 0), (2, 2)))})
    out.update({f'sleep{i}': sleep(0.4 * i) for i in range(2)})
    out.update({f'loaf{i}': loaf(blink=bool(i)) for i in range(2)})
    out.update({f'perch{i}': loaf(dy=-7, tail_swing=(0, 1, 0, -1)[i]) for i in range(4)})
    out.update({f'sphinx{i}': sphinx(flick=1.5 * i) for i in range(2)})
    out.update({f'sun{i}': sunbathe(0.4 * i) for i in range(2)})
    out.update({f'rub{i}': rub(i) for i in range(4)})
    out.update({f'munch{i}': crouch(i) for i in range(4)})
    out.update({f'nibble{i}': crouch(i, low=True) for i in range(4)})   # a leaf on the floor
    out.update({f'lick{i}': crouch(i, treat=True) for i in range(4)})
    for name, g in out.items():
        assert len(g) == SIZE and all(len(r) == SIZE for r in g), name
    return {name: shade(g) for name, g in out.items()}


def cushion_frames():
    """One cushion image per CUSHION_SQUISH entry."""
    return [shade(cushion(s)) for s in CUSHION_SQUISH]


def plant_frames():
    """The healthy plant at each sway; other states are drawn when they happen."""
    return [shade(plant(s)) for s in PLANT_SWAY]


def heart():
    return shade([list(r) for r in HEART])


def plant_layers():
    """The plant in pieces, so the app can put any state together without drawing: the pot, each
    leaf (green or dry, at each sway) and each dry leaf lying in the pot. Laid over one another in
    this order they give exactly what plant() draws: every piece is opaque except the pot's shadow."""
    def only(draw):
        c = Canvas(PLANT_W, PLANT_H)
        draw(c)
        return shade(c.g)
    out = {'plant_base': shade(plant(0, ['gone'] * len(PLANT_LEAVES)))}
    for k, (x0, y0, rx0, ry0) in enumerate(PLANT_LEAVES):
        for state in ('green', 'dry'):
            for s, sway in enumerate(PLANT_SWAY):
                def draw(c, x=x0, y=y0, rx=rx0, ry=ry0, dead=state == 'dry', sway=sway):
                    if dead:
                        y, ry = y + 1.5, ry * .75
                    x += sway * (20 - y) / 10
                    c.line((12, 20), (x, y), .6, .6, 'U' if dead else 'J')
                    c.ellipse(x, y, rx, ry, 'U' if dead else 'J')
                    c.ellipse(x + .6, y - .6, rx - .8, ry - .8, 'Y' if dead else 'L')
                out[f'plant_leaf{k}_{state}_{s}'] = only(draw)
    for i, (x, y, a) in enumerate(POT_LEAVES):
        def draw(c, x=x, y=y, a=a):
            c.tf = (a, x, y, 0, 0)
            c.ellipse(x, y, 2.8, 1.1, 'U'), c.ellipse(x + .3, y - .3, 1.9, .6, 'Y')
        out[f'pot_leaf{i}'] = only(draw)
    return out


def export(folder):
    """Draws everything once into folder/sprites.png (one sprite under the next, at 1 px per pixel)
    and folder/sprites.json (where each sprite is in it, plus the points the app lines things up
    with), so the app itself never has to draw. Run `python3 sprites.py` after changing a drawing."""
    import json, struct, zlib
    from pathlib import Path
    images = dict(frames())
    images.update({f'cushion{i}': g for i, g in enumerate(cushion_frames())})
    layers = plant_layers()
    images.update(layers)
    import random
    for _ in range(40):  # self-check: the pieces stacked up must match the whole plant, pixel for pixel
        slots = [random.choice(('green', 'dry', 'gone')) for _ in PLANT_LEAVES]
        s, pot = random.randrange(len(PLANT_SWAY)), random.randint(0, len(POT_LEAVES))
        stack = [layers['plant_base']] + [layers[f'plant_leaf{k}_{st}_{s}'] for k, st in enumerate(slots)
                                          if st != 'gone'] + [layers[f'pot_leaf{i}'] for i in range(pot)]
        got = [[next((l[y][x] for l in reversed(stack) if l[y][x][3]), (0, 0, 0, 0)) for x in range(PLANT_W)]
               for y in range(PLANT_H)]
        assert got == shade(plant(PLANT_SWAY[s], slots, pot)), (slots, s, pot)
    images.update({f'leaf{i}': g for i, g in enumerate(leaf_frames())})
    images.update({f'bin{i}': g for i, g in enumerate(bin_frames())})
    images.update({f'can{i}': g for i, g in enumerate(can_frames())})
    images['heart'] = heart()
    width = max(len(g[0]) for g in images.values())
    rows, index = [], {}
    for name, g in images.items():
        index[name] = [0, len(rows), len(g[0]), len(g)]
        rows += [[px for rgba in r for px in rgba] + [0] * 4 * (width - len(r)) for r in g]
    raw = b''.join(b'\0' + bytes(r) for r in rows)

    def chunk(kind, data):
        return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data))
    png = (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', width, len(rows), 8, 6, 0, 0, 0))
           + chunk(b'IDAT', zlib.compress(raw, 9)) + chunk(b'IEND', b''))
    Path(folder, 'sprites.png').write_bytes(png)
    Path(folder, 'sprites.json').write_text(json.dumps({
        'sprites': index, 'size': SIZE, 'walk_frames': WALK_FRAMES, 'hang_frames': HANG_FRAMES,
        'accent': ACCENT, 'hang_grab': HANG_GRAB, 'sleep_anchor': SLEEP_ANCHOR, 'perch_anchor': PERCH_ANCHOR,
        'munch_mouth': MUNCH_MOUTH, 'nibble_mouth': NIBBLE_MOUTH, 'rub_cheek': RUB_CHEEK,
        'scratch_wall': SCRATCH_WALL, 'floor': FLOOR,
        'cushion_seats': [cushion_seat(s) for s in CUSHION_SQUISH],
        'plant_size': [PLANT_W, PLANT_H], 'plant_bite': PLANT_BITE, 'plant_soil': PLANT_SOIL,
        'plant_cheek': PLANT_CHEEK, 'plant_leaves': PLANT_LEAVES, 'dry_order': DRY_ORDER,
        'pot_leaves': POT_LEAVES, 'pot_mouth': POT_MOUTH, 'leaf_size': LEAF_SIZE,
        'bin_mouth': BIN_MOUTH, 'can_pour': CAN_POUR}, indent=1))


if __name__ == '__main__':
    import os
    export(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'assets'))
