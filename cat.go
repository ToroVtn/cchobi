package main

// Her: what she does each tick, and her things on the desktop (cushion, plant, fallen leaves,
// the watering can, the bin), her speech bubble and the status card from the tray.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"fyne.io/systray"
	"github.com/jezek/xgb/xproto"
)

const (
	scale          = 2
	speed          = 1.7 // px per tick: matches how fast the paws push back in the walk cycle, so they don't skate
	tickMS         = 50
	bubbleRoom     = 130 // she keeps at least this much screen above her head
	workingTimeout = 300.0
	headroom       = 48 // room above her, inside her window, for the hearts
)

// What she does when nobody needs her: activity -> how likely, how long in seconds.
var idleActivities = []struct {
	name    string
	weight  float64
	seconds [2]float64
}{{"sleep", 5, [2]float64{120, 420}}, {"loaf", 2, [2]float64{30, 90}}, {"sphinx", 2, [2]float64{30, 90}},
	{"groom", 2, [2]float64{8, 20}}, {"sun", 2, [2]float64{60, 180}}, {"perch", 2, [2]float64{30, 120}},
	{"munch", 2, [2]float64{6, 12}}, {"plantrub", 1, [2]float64{5, 10}},
	{"siderub", 2, [2]float64{5, 10}}, {"wallscratch", 1, [2]float64{4, 8}}, {"floorscratch", 1, [2]float64{4, 8}},
	{"yawn", 1, [2]float64{3.2, 3.2}},
	{"smack", 1, [2]float64{3, 4}}, {"stretch", 1, [2]float64{4.6, 4.6}}}

var (
	follow     = map[string]string{"sleep": "stretch", "stretch": "yawn", "munch": "smack"} // waking up -> a stretch, then a yawn; after eating -> licking her lips
	groomSeq   = []int{0, 1, 0, 1, 0, 1, 2, 3, 2, 3}                                        // lick, lick, lick, wipe the face
	smackSeq   = []int{0, 0, 1, 1, 2, 2, 3, 3, 3}                                           // tongue right, up, left, mouth shut
	yawnSeq    = repeat("yawn0", 8, "yawn1", 40, "yawn0", 10)                               // one tick each, then she just sits
	stretchSeq = repeat("stretchf0", 6, "stretchf1", 30, "stretchf0", 5,                    // front stretch (down, hold, up),
		"stretchb0", 5, "stretchb1", 24, "stretchb0", 5) //                 then a back leg (out, hold, back)
	frontFacing = []string{"sit", "meow", "pet", "groom", "sleep", "yawn", "smack", "walkf", "walkb"} // never mirrored
	// Walking in 8 directions (0 = right, then clockwise as on screen): which view, and which way
	// it faces (0 = straight at you or away, no left/right).
	walkViews  = []string{"walk", "walkqf", "walkf", "walkqf", "walk", "walkqb", "walkb", "walkqb"}
	walkFacing = []int{1, 1, 0, -1, -1, -1, 0, 1}
	land       = []int{4, 4, 3, 3, 1, 1, 2, 2} // cushion squish as she lands on it
	leave      = []int{5, 5, 5, 0, 0, 1, 0}    // and as she gets off
)

func repeat(parts ...any) []string {
	var out []string
	for i := 0; i < len(parts); i += 2 {
		for range parts[i+1].(int) {
			out = append(out, parts[i].(string))
		}
	}
	return out
}

var started = time.Now()

func mono() float64 { return time.Since(started).Seconds() }

func randInt(a, b int) int { return a + rand.Intn(b-a+1) }

func abs(v int) int { return max(v, -v) }

// ---- Prop: something of hers on the desktop. Drag it to move it; a plain click (left or
// right) calls clicked ----

type Prop struct {
	*Win
	frames           []*image.NRGBA
	seq              []int
	frame            int
	grab             *[2]int // where you're holding it, while you drag it
	moved            bool
	trail            []trailPoint // recent positions while dragged, to know how fast it was thrown
	dropped, clicked func()
	spot             [2]int // a fallen leaf: where it lies
}

type trailPoint struct {
	t    float64
	x, y int
}

func newProp(frames []*image.NRGBA) *Prop {
	b := frames[0].Bounds()
	p := &Prop{Win: newWin(b.Dx()*scale, b.Dy()*scale), frames: frames, frame: -1,
		dropped: func() {}, clicked: func() {}}
	var shape []xproto.Rectangle
	for _, f := range frames {
		shape = append(shape, shapeOf(f, 0, 0, scale, false)...)
	}
	p.setShape(shape)
	p.Win.press, p.Win.motion, p.Win.release = p.onPress, p.onMotion, p.onRelease
	p.draw(0)
	return p
}

func (p *Prop) draw(frame int) {
	if frame != p.frame {
		p.frame = frame
		clear(p.img)
		blit(p.img, p.frames[frame], 0, 0, scale, false, 1)
		p.flush()
	}
}

func (p *Prop) play(seq []int) { p.seq = slices.Clone(seq) }

func (p *Prop) step(rest int) {
	if len(p.seq) > 0 {
		rest, p.seq = p.seq[0], p.seq[1:]
	}
	p.draw(rest)
}

// point: the screen position of a pixel of the prop's sprite.
func (p *Prop) point(x, y float64) (float64, float64) {
	return float64(p.x) + x*scale, float64(p.y) + y*scale
}

func (p *Prop) center() (float64, float64) { return float64(p.x + p.w/2), float64(p.y + p.h/2) }

func (p *Prop) track() {
	now := mono()
	p.trail = slices.DeleteFunc(p.trail, func(t trailPoint) bool { return now-t.t >= 0.12 })
	p.trail = append(p.trail, trailPoint{now, p.x, p.y})
}

// velocity: how fast it was moving when you let go, in pixels per second (sideways).
func (p *Prop) velocity() float64 {
	if len(p.trail) < 2 {
		return 0
	}
	a, b := p.trail[0], p.trail[len(p.trail)-1]
	return float64(b.x-a.x) / max(b.t-a.t, 1e-3)
}

func (p *Prop) onPress(button, rx, ry, _, _ int) {
	if button == 3 {
		p.clicked()
	} else if button == 1 {
		p.grab, p.moved = &[2]int{rx - p.x, ry - p.y}, false
	}
}

func (p *Prop) onMotion(rx, ry, _, _ int) {
	if p.grab != nil {
		tx, ty := rx-p.grab[0], ry-p.grab[1]
		p.moved = p.moved || abs(tx-p.x)+abs(ty-p.y) > 4
		if p.moved {
			p.move(tx, ty)
			p.track()
		}
	}
}

func (p *Prop) onRelease(button, _, _ int) {
	if button != 1 || p.grab == nil {
		return
	}
	p.grab = nil
	if p.moved {
		p.dropped()
	} else {
		p.clicked()
	}
}

// ---- her plant, put together from the garden's state: which leaves are green, dry or gone,
// and how many dry ones lie in the pot ----

type Plant struct {
	*Prop
	sheet    *Sheet
	garden   *Garden
	sway     []int // queued shakes (the leaves perk up after watering)
	drawnKey string
	held     *Prop // a dry leaf you've pulled off and are holding
	pluck    func(k int) *Prop
	release  func(*Prop)
}

func newPlant(s *Sheet, g *Garden) *Plant {
	healthy := make([]*image.NRGBA, 3) // the healthy plant's outline is where it catches clicks
	for sway := range healthy {
		img := image.NewRGBA(image.Rect(0, 0, s.PlantSize[0], s.PlantSize[1]))
		blit(img, s.get("plant_base"), 0, 0, 1, false, 1)
		for k := range s.PlantLeaves {
			blit(img, s.get(fmt.Sprintf("plant_leaf%d_green_%d", k, sway)), 0, 0, 1, false, 1)
		}
		healthy[sway] = toNRGBA(img)
	}
	p := &Plant{Prop: newProp(healthy), sheet: s, garden: g}
	p.Win.press, p.Win.motion, p.Win.release = p.onPress, p.onMotion, p.onRelease
	return p
}

func toNRGBA(img *image.RGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Rect)
	for i := 0; i < len(img.Pix); i += 4 {
		if a := img.Pix[i+3]; a > 0 {
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] =
				uint8(uint32(img.Pix[i])*255/uint32(a)), uint8(uint32(img.Pix[i+1])*255/uint32(a)), uint8(uint32(img.Pix[i+2])*255/uint32(a)), a
		}
	}
	return out
}

// showState draws the plant as the garden has it now; shake 1 or 2 sways its leaves.
func (p *Plant) showState(shake int) {
	if len(p.sway) > 0 {
		shake, p.sway = p.sway[0], p.sway[1:]
	}
	pot := min(p.garden.Pot, len(p.sheet.PotLeaves))
	key := fmt.Sprint(p.garden.Leaves, pot, shake)
	if key == p.drawnKey {
		return
	}
	p.drawnKey = key
	clear(p.img)
	blit(p.img, p.sheet.get("plant_base"), 0, 0, scale, false, 1)
	for k, leaf := range p.garden.Leaves {
		if leaf.State != "gone" {
			blit(p.img, p.sheet.get(fmt.Sprintf("plant_leaf%d_%s_%d", k, leaf.State, shake)), 0, 0, scale, false, 1)
		}
	}
	for i := range pot {
		blit(p.img, p.sheet.get(fmt.Sprintf("pot_leaf%d", i)), 0, 0, scale, false, 1)
	}
	p.flush()
}

// dryLeafAt: the dry leaf under a point of the window, or -1.
func (p *Plant) dryLeafAt(px, py int) int {
	x, y := float64(px)/scale, float64(py)/scale
	for _, k := range p.garden.dry() {
		l := p.sheet.PlantLeaves[k]
		lx, ly, rx, ry := l[0], l[1]+1.5, l[2], l[3]*.75 // dry leaves droop and shrivel
		if math.Pow((x-lx)/(rx+1), 2)+math.Pow((y-ly)/(ry+1), 2) <= 1 {
			return k
		}
	}
	return -1
}

func (p *Plant) hold(rx, ry int) {
	half := p.sheet.LeafSize * scale / 2
	p.held.move(rx-half, ry-half)
	p.held.track()
}

func (p *Plant) onPress(button, rx, ry, x, y int) {
	if k := p.dryLeafAt(x, y); button == 1 && k >= 0 {
		p.held = p.pluck(k) // pull it off: it comes away in your hand
		p.hold(rx, ry)
		return
	}
	p.Prop.onPress(button, rx, ry, x, y)
}

func (p *Plant) onMotion(rx, ry, x, y int) {
	if p.held != nil {
		p.hold(rx, ry)
		return
	}
	p.Prop.onMotion(rx, ry, x, y)
}

func (p *Plant) onRelease(button, rx, ry int) {
	if p.held != nil {
		leaf := p.held
		p.held = nil
		p.release(leaf)
		return
	}
	p.Prop.onRelease(button, rx, ry)
}

// ---- the card that pops up when you click the tray icon: a watering can pouring next to when
// the plant was last watered, and a dry leaf next to roughly when a leaf could next dry out ----

type Card struct {
	*Win
	garden *Garden
	cans   []*image.NRGBA
	leaf   *image.NRGBA
	at     float64
	frame  int
}

func newCard(s *Sheet, g *Garden) *Card {
	c := &Card{Win: newWin(250, 136), garden: g, leaf: s.get("leaf0")}
	for i := 2; i < 6; i++ { // tipped over, droplets falling
		can := s.get(fmt.Sprintf("can%d", i))
		o := can.Rect.Min
		c.cans = append(c.cans, can.SubImage(image.Rect(o.X+4, o.Y+6, o.X+36, o.Y+40)).(*image.NRGBA))
	}
	c.Win.press = func(int, int, int, int, int) { c.hide() }
	return c
}

func (c *Card) popup() {
	m := monitors()[0]
	g := workArea(m.x+m.w/2, m.y+m.h/2)
	c.move(g.x+g.w-c.w-12, g.y+g.h-c.h-12)
	c.at, c.frame = mono(), -1
	c.step()
	c.show()
	c.raise()
}

func (c *Card) step() {
	t := mono() - c.at
	if t > 6 {
		c.hide()
		return
	}
	frame := int(t/0.13) % len(c.cans)
	if frame == c.frame {
		return
	}
	c.frame = frame
	clear(c.img)
	box(c.img, 0, 0, c.w, c.h, accent)
	watered, check := c.garden.describe(unixNow())
	blit(c.img, c.cans[frame], 8, 2, scale, false, 1)
	lh := lineHeight()
	drawText(c.img, []string{watered}, 84, 2+(68-lh)/2, accent)
	blit(c.img, c.leaf, 8+(64-40)/2, 86, scale*2, false, 1)
	drawText(c.img, []string{check}, 84, 76+(60-lh)/2, accent)
	c.flush()
}

// ---- her ----

type Pending struct { // a session that wants you
	session, project, transcript, text string
	vscode                             bool
	detail                             *string
}

type Food struct {
	kind string // plant, pot or floor
	leaf *Prop  // floor: which leaf
}

type Activity struct {
	name           string
	seconds, until float64
	arrived        bool
	t0             int
	facing         int // 1 right, -1 left, 0 whichever way
	spot           [2]float64
	food           Food
}

type Flight struct { // a leaf falling or flying through the air
	leaf               *Prop
	start, end         [2]float64
	t0, ticks          int
	arc, wobble        float64
	fall, catch, begun bool
	started, landed    func()
	done               bool
}

type Pet struct {
	sheet                                        *Sheet
	win, bubble                                  *Win
	cushion, can, bin                            *Prop
	plant                                        *Plant
	card                                         *Card
	garden                                       *Garden
	muted                                        map[string][]string // sounds switched off in Settings
	sounds                                       *Sounds
	nextPurr, purrEnd, purrVolume                float64
	purring                                      *os.Process // the purr playing now, to fade out when she stops
	leaves                                       []*Prop     // dry leaves lying about
	flights                                      []*Flight
	working                                      map[string]float64 // session -> last time we heard it working
	pending                                      []*Pending         // newest last
	meowUntil, petUntil, treatUntil, relishUntil float64
	tick                                         int
	facingRight                                  bool
	frame, walkView                              string
	drawn                                        string
	shapes                                       map[string][]xproto.Rectangle
	press                                        *[2]int
	dragging, hover, expanded, pouring           bool
	pointerX                                     int
	hearts                                       []heart
	windows                                      []rect // open windows, bottom to top, to perch on and rub against; refreshed in the background
	bubbleKey, tooltip                           string
	activity                                     *Activity
	x, y                                         float64      // her sprite's top left on screen
	targetSpot                                   [2]float64   // where she's wandering to while Claude works
	route                                        [][2]float64 // where she steps from screen to screen on her way somewhere
	routeTo                                      rect         // the screen that route leads to
}

type heart struct {
	born int
	dx   float64
}

func newPet(s *Sheet, g *Garden, muted map[string][]string) *Pet {
	accent = color.NRGBA{s.Accent[0], s.Accent[1], s.Accent[2], 255}
	frames := func(prefix string, n int) []*image.NRGBA {
		var out []*image.NRGBA
		for i := range n {
			out = append(out, s.get(fmt.Sprint(prefix, i)))
		}
		return out
	}
	p := &Pet{sheet: s, garden: g, muted: muted, sounds: &Sounds{}, working: map[string]float64{},
		shapes: map[string][]xproto.Rectangle{}, frame: "sleep0", walkView: "walk"}
	p.cushion = newProp(frames("cushion", len(s.CushionSeat)))
	p.plant = newPlant(s, g)
	p.can = newProp(frames("can", 6)) // only out while you're watering
	p.bin = newProp(frames("bin", 2)) // only out while you're throwing leaves away
	p.card = newCard(s, g)
	cat := s.Size * scale
	p.win = newWin(cat, cat+headroom) // created last, so she's above her things
	p.bubble = newWin(1, 1)
	p.win.press, p.win.motion, p.win.release = p.onPress, p.onMotion, p.onRelease
	p.win.leave = func() { p.hover = false }
	p.bubble.press = p.onBubble
	p.plant.pluck, p.plant.release = p.pluckLeaf, func(leaf *Prop) { p.glide(leaf, nil, true) }
	p.plant.clicked = p.water // a click on the plant (left or right) waters it
	p.cushion.dropped = func() { p.raise(); p.save() }
	p.plant.dropped = p.cushion.dropped
	return p
}

// start puts her things out, and her on her cushion: she starts the day in bed.
func (p *Pet) start() {
	p.cushion.show()
	p.plant.showState(0)
	p.plant.show()
	for _, spot := range p.garden.Fallen {
		p.leaves = append(p.leaves, p.makeLeaf(spot[0], spot[1], true))
	}
	p.activity = &Activity{name: "sleep", seconds: uniform(60, 300), arrived: true, until: mono() + uniform(20, 120)}
	p.x, p.y, _ = p.target(p.activity)
	p.targetSpot = [2]float64{p.x, p.y}
	p.moveCat()
	p.render()
	p.win.show()
	p.raise()
}

func (p *Pet) save() {
	p.garden.Fallen = nil
	for _, leaf := range p.leaves {
		p.garden.Fallen = append(p.garden.Fallen, leaf.spot)
	}
	for _, names := range p.muted {
		sort.Strings(names)
	}
	b, _ := json.Marshal(map[string]any{"cushion": [2]int{p.cushion.x, p.cushion.y},
		"plant": [2]int{p.plant.x, p.plant.y}, "garden": p.garden, "muted": p.muted})
	if os.WriteFile(stateF+".new", b, 0o644) == nil { // never leave a half-written file behind
		os.Rename(stateF+".new", stateF)
	}
}

// ---- events from Claude Code (and from Settings) ----

func str(v any) string { s, _ := v.(string); return s }

func (p *Pet) handle(e map[string]any) {
	switch e["cchobi"] { // from Settings
	case "muted":
		b, _ := json.Marshal(e["muted"])
		json.Unmarshal(b, &p.muted)
		p.save()
		return
	case "sounds":
		go scanSounds(func(s *Sounds) { calls <- func() { p.sounds = s } })
		return
	}
	sid, now := str(e["session_id"]), mono()
	switch str(e["hook_event_name"]) {
	case "UserPromptSubmit", "PostToolUse": // you answered / it's busy again
		p.working[sid] = now
		p.dismiss(sid)
	case "Notification", "Stop":
		delete(p.working, sid)
		p.dismiss(sid) // re-added, so it becomes the newest
		text := str(e["message"])
		if text == "" {
			text = "done!"
		}
		project := ""
		if cwd := str(e["cwd"]); cwd != "" {
			project = filepath.Base(cwd)
		}
		p.pending = append(p.pending, &Pending{session: sid, project: project, vscode: e["vscode"] == true,
			transcript: str(e["transcript_path"]), text: text})
		p.expanded = false
		p.meow()
	case "SessionEnd":
		delete(p.working, sid)
		p.dismiss(sid)
	}
}

func (p *Pet) dismiss(sid string) {
	p.pending = slices.DeleteFunc(p.pending, func(q *Pending) bool { return q.session == sid })
}

func (p *Pet) popPending() *Pending {
	if len(p.pending) == 0 {
		return nil
	}
	last := p.pending[len(p.pending)-1]
	p.pending = p.pending[:len(p.pending)-1]
	return last
}

func (p *Pet) unmuted(kind string) []Sound {
	var out []Sound
	for _, s := range (*p.sounds)[kind] {
		if !slices.Contains(p.muted[kind], s.name) {
			out = append(out, s)
		}
	}
	return out
}

func (p *Pet) meow() {
	meows := p.unmuted("meows")
	if len(meows) == 0 { // all switched off in Settings: she meows silently
		p.meowUntil = mono() + 1.5
		return
	}
	s := meows[rand.Intn(len(meows))]
	p.meowUntil = mono() + max(1.5, s.seconds) // her mouth moves for as long as the sound plays
	play(s.file, 1)
}

// purr keeps her purring: a random variant, at a random soft volume, each one fading in while the
// last fades out.
func (p *Pet) purr() {
	now, purrs := mono(), p.unmuted("purrs")
	if len(purrs) > 0 && now >= p.nextPurr {
		s := purrs[rand.Intn(len(purrs))]
		p.purrVolume = uniform(0.8, 0.92)
		p.purring = play(s.file, p.purrVolume)
		p.purrEnd = now + s.seconds
		p.nextPurr = p.purrEnd - fadeFor(s.seconds)*uniform(0.6, 0.9)
	}
}

// stopPurr fades out the purr that's still playing, if any, and lets the next one start right away.
func (p *Pet) stopPurr() {
	if p.purring != nil && mono() < p.purrEnd {
		fadeOut(p.purring, p.purrVolume)
	}
	p.purring, p.nextPurr = nil, 0
}

// ---- what she's doing ----

func (p *Pet) step() {
	p.tick++
	now := mono()
	// ponytail: no hook fires when you interrupt Claude with Esc, so a session that's been
	// silent for 5 min stops counting as working.
	for s, t := range p.working {
		if now-t >= workingTimeout {
			delete(p.working, s)
		}
	}
	walking := fmt.Sprintf("%s%d", p.walkView, p.tick/2%p.sheet.WalkFrames)
	breath := p.tick / 25 % 2 // slow breathing
	busy := p.dragging || p.hover || now < p.petUntil || now < p.relishUntil || now < p.meowUntil ||
		len(p.pending) > 0 || len(p.working) > 0
	if busy && p.activity != nil {
		p.endActivity()
	}
	var frame string
	purring := false
	switch {
	case p.dragging:
		frame = fmt.Sprintf("hang%d", p.tick/3%p.sheet.HangFrames)
	case now < p.meowUntil:
		frame = map[bool]string{true: "meow", false: "sit"}[p.tick/4%2 == 1]
	case now < p.petUntil:
		frame = "pet"
		purring = true
	case now < p.treatUntil: // nothing interrupts a treat, not even your pointer
		frame = fmt.Sprintf("lick%d", p.tick/3%4)
	case now < p.relishUntil: // that was good: licking her lips
		frame = fmt.Sprintf("smack%d", smackSeq[p.tick/2%len(smackSeq)])
	case p.hover: // facing you, cheek pushed towards the pointer
		frame = fmt.Sprintf("rubf%d", p.tick/3%4)
		p.facingRight = p.pointerX > int(p.x)+p.win.w/2
		purring = true
	case len(p.pending) > 0:
		frame = "sit"
	case len(p.working) > 0:
		frame = walking
		if p.walkVia(p.targetSpot[0], p.targetSpot[1]) {
			p.targetSpot = p.randomSpot()
		}
	default:
		frame = p.idle(now, walking, breath)
	}
	if purring {
		p.purr()
	} else {
		p.stopPurr()
	}

	act := ""
	if p.activity != nil && p.activity.arrived {
		act = p.activity.name
	}
	if act == "sleep" {
		p.cushion.step(1 + breath)
	} else {
		p.cushion.step(0)
	}
	shake := 0 // its leaves shake while she's at it
	if act == "plantrub" || (act == "munch" && p.activity.food.kind != "floor") {
		shake = 1 + p.tick/3%2
	}
	p.plant.showState(shake)
	p.stepFlights()
	if p.tick%20 == 0 {
		p.gardenTick()
	}
	p.hearts = slices.DeleteFunc(p.hearts, func(h heart) bool { return p.tick-h.born >= 30 })
	p.frame = frame
	p.render()
	if p.pouring {
		p.pour()
	}
	if p.card.shown {
		p.card.step()
	}
}

func (p *Pet) idle(now float64, walking string, breath int) string {
	act := p.activity
	if act == nil || (act.arrived && now > act.until) {
		previous := ""
		if act != nil {
			if act.name == "munch" {
				p.eat(act.food) // finished her snack: that leaf is gone
			}
			previous = act.name
		}
		p.endActivity()
		act = p.pickActivity(previous)
		p.activity = act
	}
	if act.name == "munch" && !p.foodLeft(act.food) { // someone took it first
		act = p.pickActivity("")
		p.activity = act
	}
	x, y, facing := p.target(act)
	if !act.arrived {
		if !p.walkVia(x, y) {
			return walking
		}
		act.arrived, act.until, act.t0 = true, now+act.seconds, p.tick
		if act.name == "sleep" {
			p.cushion.play(land)
			p.raise()
		}
	}
	p.x, p.y = x, y // stay on things that move: a squishing cushion, a dragged plant
	p.moveCat()
	if facing != 0 {
		p.facingRight = facing > 0
	}
	t := p.tick
	switch act.name {
	case "yawn", "stretch": // played through once from when she started
		seq, rest := yawnSeq, "sit"
		if act.name == "stretch" {
			seq, rest = stretchSeq, "walk0"
		}
		if k := t - act.t0; k < len(seq) {
			return seq[k]
		}
		return rest
	case "munch": // head right down for a leaf on the floor
		if act.food.kind == "floor" {
			return fmt.Sprintf("nibble%d", t/3%4)
		}
		return fmt.Sprintf("munch%d", t/3%4)
	case "sleep":
		return fmt.Sprintf("sleep%d", breath)
	case "sun":
		return fmt.Sprintf("sun%d", breath)
	case "smack":
		return fmt.Sprintf("smack%d", smackSeq[t/2%len(smackSeq)])
	case "loaf":
		if t%120 < 4 { // slow blink now and then
			return "loaf1"
		}
		return "loaf0"
	case "sphinx":
		return fmt.Sprintf("sphinx%d", t/12%2)
	case "groom":
		return fmt.Sprintf("groom%d", groomSeq[t/3%len(groomSeq)])
	case "perch":
		return fmt.Sprintf("perch%d", t/8%4)
	case "wallscratch":
		return fmt.Sprintf("scratchw%d", t/3%4)
	case "floorscratch":
		return fmt.Sprintf("scratchf%d", t/3%4)
	}
	return fmt.Sprintf("rub%d", t/3%4) // plantrub, siderub
}

func (p *Pet) pickActivity(previous string) *Activity {
	name, ok := follow[previous]
	if !ok {
		hour, total := time.Now().Hour(), 0.0
		type option struct {
			name   string
			weight float64
		}
		var options []option
		for _, a := range idleActivities {
			if (a.name != "sun" || (hour >= 8 && hour < 18)) && (a.name != "perch" || len(p.perches()) > 0) &&
				(a.name != "siderub" || len(p.sides()) > 0) && (a.name != "munch" || len(p.foods()) > 0) &&
				(a.name != "wallscratch" || len(p.sides()) > 0) && (a.name != "floorscratch" || len(p.floors()) > 0) {
				options = append(options, option{a.name, a.weight})
				total += a.weight
			}
		}
		r := rand.Float64() * total
		for _, o := range options {
			if name = o.name; r < o.weight {
				break
			}
			r -= o.weight
		}
	}
	act := &Activity{name: name}
	for _, a := range idleActivities {
		if a.name == name {
			act.seconds = uniform(a.seconds[0], a.seconds[1])
		}
	}
	switch name {
	case "munch":
		foods := p.foods()
		act.food = foods[rand.Intn(len(foods))]
	case "perch":
		perches := p.perches()
		w := perches[rand.Intn(len(perches))]
		x, y := p.place(p.sheet.PerchAnchor, float64(randInt(w.x+40, w.x+w.w-40)), float64(w.y), true)
		act.spot = [2]float64{x, y}
		act.facing = []int{1, -1}[rand.Intn(2)]
	case "siderub", "wallscratch": // her cheek, or her claws, against a side, somewhere along it
		sides := p.sides()
		e := sides[rand.Intn(len(sides))]
		anchor := p.sheet.RubCheek
		if name == "wallscratch" {
			anchor = p.sheet.ScratchWall
		}
		x, y := p.place(anchor, float64(e.x), float64(randInt(e.y0, e.y1)), e.facing > 0)
		act.spot, act.facing = [2]float64{x, y}, e.facing
	case "floorscratch":
		floors := p.floors()
		f := floors[rand.Intn(len(floors))]
		x, y := p.place(p.sheet.Floor, float64(randInt(f.x+40, f.x+f.w-40)), float64(f.y), true)
		act.spot = [2]float64{x, y}
		act.facing = []int{1, -1}[rand.Intn(2)]
	case "loaf", "sphinx", "groom", "sun":
		act.spot = p.randomSpot()
	case "yawn", "smack", "stretch": // right where she is
		act.spot = [2]float64{p.x, p.y}
	}
	if slices.Contains([]string{"perch", "siderub", "wallscratch", "floorscratch"}, name) || rand.Float64() < .3 {
		p.refreshLayout() // windows move; look again for next time
	}
	return act
}

func (p *Pet) endActivity() {
	act := p.activity
	p.activity = nil
	if act != nil && act.name == "sleep" && act.arrived {
		p.cushion.play(leave)
	}
}

// target: where she should be for an activity, and which way she faces there (0: whichever).
func (p *Pet) target(act *Activity) (float64, float64, int) {
	s := p.sheet
	switch {
	case act.name == "sleep":
		seat := s.CushionSeat[max(p.cushion.frame, 0)]
		x, y := p.cushion.point(seat[0], seat[1])
		x, y = p.place(s.SleepAnchor, x, y, true)
		return x, y, 0
	case act.name == "munch" && act.food.kind == "floor": // a fallen leaf: head right down to it
		cx, cy := act.food.leaf.center()
		fromLeft := p.x+float64(p.win.w)/2 < cx
		x, y := p.place(s.NibbleMouth, cx, cy, fromLeft)
		return x, y, side(fromLeft)
	case act.name == "munch" || act.name == "plantrub":
		pl := p.plant
		fromLeft := p.x+float64(p.win.w)/2 < float64(pl.x)+float64(pl.w)/2
		at, anchor := s.PlantCheek, s.RubCheek
		if act.name == "munch" {
			at, anchor = s.PlantBite, s.MunchMouth
			if act.food.kind == "pot" {
				at = s.PlantSoil
			}
		}
		if !fromLeft {
			at[0] = float64(s.PlantSize[0]) - at[0]
		}
		x, y := pl.point(at[0], at[1])
		x, y = p.place(anchor, x, y, fromLeft)
		return x, y, side(fromLeft)
	}
	return act.spot[0], act.spot[1], act.facing
}

func side(right bool) int {
	if right {
		return 1
	}
	return -1
}

// place: where her sprite goes so that one of its pixels lands on a screen point.
func (p *Pet) place(anchor [2]float64, x, y float64, facingRight bool) (float64, float64) {
	ax := anchor[0]
	if !facingRight {
		ax = float64(p.sheet.Size) - ax
	}
	return x - ax*scale, y - anchor[1]*scale
}

// ---- her plant's dry leaves ----

// foods: dry leaves she could eat: on the plant, in the pot, or lying on the floor.
func (p *Pet) foods() []Food {
	var out []Food
	if len(p.garden.dry()) > 0 {
		out = append(out, Food{kind: "plant"})
	}
	if p.garden.Pot > 0 {
		out = append(out, Food{kind: "pot"})
	}
	for _, leaf := range p.leaves {
		out = append(out, Food{"floor", leaf})
	}
	return out
}

func (p *Pet) foodLeft(f Food) bool {
	switch f.kind {
	case "plant":
		return len(p.garden.dry()) > 0
	case "pot":
		return p.garden.Pot > 0
	}
	return slices.Contains(p.leaves, f.leaf)
}

func (p *Pet) eat(f Food) {
	switch {
	case f.kind == "plant" && len(p.garden.dry()) > 0:
		p.garden.pick(p.garden.dry()[0])
	case f.kind == "pot" && p.garden.Pot > 0:
		p.garden.Pot--
	case f.kind == "floor" && slices.Contains(p.leaves, f.leaf):
		p.removeLeaf(f.leaf)
		f.leaf.destroy()
	}
	p.save()
}

// makeLeaf: a dry leaf off the plant, at (x, y) on screen. Drag it and let go: it glides down.
func (p *Pet) makeLeaf(x, y int, show bool) *Prop {
	frames := []*image.NRGBA{p.sheet.get("leaf0"), p.sheet.get("leaf1"), p.sheet.get("leaf2"), p.sheet.get("leaf3")}
	leaf := newProp(frames)
	leaf.spot = [2]int{x, y}
	leaf.dropped = func() { p.glide(leaf, nil, true) }
	if show {
		leaf.move(x, y)
		leaf.show()
	}
	return leaf
}

func (p *Pet) removeLeaf(leaf *Prop) {
	p.leaves = slices.DeleteFunc(p.leaves, func(l *Prop) bool { return l == leaf })
}

// leafSpot: the screen position for a leaf window centred on a point of the plant's sprite.
func (p *Pet) leafSpot(x, y float64) (float64, float64) {
	half := float64(p.sheet.LeafSize) / 2
	return p.plant.point(x-half, y-half)
}

// pluckLeaf: you pull a dry leaf off the plant: it leaves a gap, and it's in your hand.
func (p *Pet) pluckLeaf(k int) *Prop {
	l := p.sheet.PlantLeaves[k]
	sx, sy := p.leafSpot(l[0], l[1])
	p.garden.pick(k)
	p.save()
	return p.makeLeaf(int(sx), int(sy), true)
}

func (p *Pet) intoPot(leaf *Prop) {
	p.removeLeaf(leaf)
	leaf.destroy()
	p.garden.Pot++
	p.plant.sway = []int{1, 2}
	p.save()
}

// glide: let go of a leaf. It carries on a little the way it was moving, flutters down to the
// floor (the line the pot stands on) and lands, or drops into the pot if it passes over it.
func (p *Pet) glide(leaf *Prop, push *float64, catch bool) {
	vx := leaf.velocity()
	if push != nil {
		vx = *push
	}
	half := float64(p.sheet.LeafSize) / 2
	floor := float64(p.plant.y) + (float64(p.sheet.PlantSize[1])-half-1)*scale
	sx, sy := float64(leaf.x), float64(leaf.y)
	if sy > floor-10 { // let go below the floor line: settle a little lower
		floor = sy + 24
	}
	drift := max(-160, min(160, vx*0.35)) + uniform(-20, 20)
	ticks := int(max(14, (floor-sy)/3.5))
	for _, f := range p.flights {
		if f.leaf == leaf {
			f.done = true
		}
	}
	landed := func() {
		leaf.spot = [2]int{leaf.x, leaf.y}
		if !slices.Contains(p.leaves, leaf) {
			p.leaves = append(p.leaves, leaf)
		}
		p.save()
		p.raise()
	}
	p.fly(&Flight{leaf: leaf, start: [2]float64{sx, sy}, end: [2]float64{sx + drift, floor}, ticks: ticks,
		wobble: 10, fall: true, catch: catch, landed: landed})
}

// gardenTick: time passes for the plant. A leaf that drops off lands in the pot half the time;
// otherwise it glides off to the side, away from the pot.
func (p *Pet) gardenTick() {
	watered, check := p.garden.describe(unixNow())
	if tip := "💧 " + watered + "\n🍂 " + check; tip != p.tooltip { // Plasma's tray tooltips are text only
		p.tooltip = tip
		systray.SetTooltip(tip)
	}
	for _, k := range p.garden.update(unixNow()) {
		l := p.sheet.PlantLeaves[k]
		sx, sy := p.leafSpot(l[0], l[1])
		leaf := p.makeLeaf(int(sx), int(sy), true)
		if rand.Float64() < 0.5 {
			m := p.sheet.PotMouth
			mx, my := p.leafSpot((m[0]+m[2])/2, (m[1]+m[3])/2)
			p.fly(&Flight{leaf: leaf, start: [2]float64{sx, sy}, end: [2]float64{mx, my}, ticks: 18, wobble: 4,
				fall: true, landed: func() { p.intoPot(leaf) }})
		} else {
			away := 1.0
			if l[0] < float64(p.sheet.PlantSize[0])/2 {
				away = -1
			}
			push := away * uniform(250, 400)
			p.glide(leaf, &push, false)
		}
	}
	p.save()
}

// fly sends a leaf through the air: fluttering down (fall: speeds up; catch: drops into the pot if
// it passes over it) or tossed in an arc.
func (p *Pet) fly(f *Flight) {
	if !f.leaf.shown {
		f.leaf.move(int(f.start[0]), int(f.start[1]))
	}
	f.t0 += p.tick
	p.flights = append(p.flights, f)
}

func (p *Pet) inPotMouth(leaf *Prop) bool {
	m := p.sheet.PotMouth
	ax, ay := p.plant.point(m[0], m[1])
	bx, by := p.plant.point(m[2], m[3])
	cx, cy := leaf.center()
	return ax <= cx && cx <= bx && ay <= cy && cy <= by
}

func (p *Pet) stepFlights() {
	for _, f := range slices.Clone(p.flights) {
		pr := float64(p.tick-f.t0) / float64(f.ticks)
		if f.done || pr < 0 {
			continue
		}
		leaf := f.leaf
		if !f.begun { // its turn to go
			f.begun = true
			leaf.show()
			leaf.raise()
			if f.started != nil {
				f.started()
			}
		}
		if pr >= 1 {
			leaf.move(int(f.end[0]), int(f.end[1]))
			leaf.draw(0) // lies flat once it's down
			f.done = true
			if f.landed != nil {
				f.landed()
			}
			continue
		}
		down := pr
		if f.fall {
			down = math.Pow(pr, 1.3) // a falling leaf picks up speed
		}
		x := f.start[0] + (f.end[0]-f.start[0])*pr + f.wobble*math.Sin(pr*3*math.Pi)
		y := f.start[1] + (f.end[1]-f.start[1])*down - f.arc*4*pr*(1-pr)
		leaf.move(int(x), int(y))
		leaf.draw(1 + p.tick/3%3)                       // tumbling
		if f.catch && pr > 0.15 && p.inPotMouth(leaf) { // fell into the pot
			f.done = true
			p.intoPot(leaf)
		}
	}
	p.flights = slices.DeleteFunc(p.flights, func(f *Flight) bool { return f.done })
}

// throwAway: a bin appears by the plant and every dry leaf (on the plant, in the pot, lying
// about) is tossed into it, one by one.
func (p *Pet) throwAway() {
	if p.bin.shown {
		return
	}
	type throw struct {
		leaf    *Prop
		start   [2]float64
		started func()
	}
	var throws []throw
	for _, k := range p.garden.dry() {
		l := p.sheet.PlantLeaves[k]
		x, y := p.leafSpot(l[0], l[1])
		throws = append(throws, throw{p.makeLeaf(0, 0, false), [2]float64{x, y}, func() { p.garden.pick(k) }})
	}
	for i := range p.garden.Pot {
		l := p.sheet.PotLeaves[i%len(p.sheet.PotLeaves)]
		x, y := p.leafSpot(l[0], l[1])
		throws = append(throws, throw{p.makeLeaf(0, 0, false), [2]float64{x, y}, func() { p.garden.Pot-- }})
	}
	for _, leaf := range p.leaves { // these stay lying where they are until their turn
		throws = append(throws, throw{leaf, [2]float64{float64(leaf.x), float64(leaf.y)}, func() { p.removeLeaf(leaf) }})
	}
	if len(throws) == 0 {
		return
	}
	pl := p.plant
	p.bin.move(pl.x+pl.w+24, pl.y+pl.h-p.bin.h)
	p.bin.draw(1) // lid open
	p.bin.show()
	half := float64(p.sheet.LeafSize) / 2
	mx, my := p.bin.point(p.sheet.BinMouth[0], p.sheet.BinMouth[1])
	for n, t := range throws {
		p.fly(&Flight{leaf: t.leaf, start: t.start, end: [2]float64{mx - half*scale, my - half*scale}, ticks: 16,
			arc: 60, t0: n * 8, started: t.started, landed: t.leaf.destroy})
	}
	later(time.Duration(len(throws)*8+16+6)*tickMS*time.Millisecond, func() {
		p.bin.draw(0) // lid shut
		later(600*time.Millisecond, p.bin.hide)
		p.save()
	})
}

// water: a watering can tips over the plant and pours. Dry leaves stay dry, but it keeps the
// plant from drying at its next check, if that comes within 2 hours.
func (p *Pet) water() {
	if p.can.shown {
		return
	}
	x, y := p.plant.point(12, 5) // the top of the plant
	p.can.move(int(x-p.sheet.CanPour[0]*scale), int(y-p.sheet.CanPour[1]*scale))
	p.can.draw(0)
	p.can.show()
	p.can.raise()
	pourTicks := 32 // with no sound: 1.6 s of pouring
	if sounds := p.unmuted("water"); len(sounds) > 0 {
		// it pours for exactly as long as the sound, which starts as the can tips over
		s := sounds[rand.Intn(len(sounds))]
		pourTicks = max(8, int(math.Round(s.seconds*1000/tickMS)))
		later(7*tickMS*time.Millisecond, func() { play(s.file, uniform(0.8, 0.95)) })
	}
	seq := []int{0, 0, 0, 0, 1, 1, 1}
	for i := range pourTicks { // droplets falling
		seq = append(seq, 2+i/2%4)
	}
	p.can.play(append(seq, 1, 1, 1, 0, 0, 0))
	p.pouring = true
}

func (p *Pet) pour() {
	p.can.step(0)
	if len(p.can.seq) == 0 {
		p.pouring = false
		p.can.hide()
		p.garden.water(unixNow())
		p.plant.sway = []int{1, 1, 2, 2, 1, 1, 2, 2} // the leaves perk up
		p.save()
	}
}

func (p *Pet) treat() {
	p.treatUntil = mono() + 8
	p.relishUntil = p.treatUntil + 3 // and then she licks her lips
}

func (p *Pet) refreshLayout() {
	go func() {
		windows, areas := layout()
		calls <- func() {
			p.windows = windows
			if areas != nil {
				workAreas = areas
			}
		}
	}()
}

// visible: the parts of the line from lo to hi that none of the windows above cover. hides
// gives the stretch of the line a window covers (ok false: it doesn't cross the line).
func visible(lo, hi int, above []rect, hides func(rect) (a, b int, ok bool)) [][2]int {
	parts := [][2]int{{lo, hi}}
	for _, w := range above {
		a, b, ok := hides(w)
		if !ok {
			continue
		}
		var left [][2]int
		for _, p := range parts {
			if a > p[0] {
				left = append(left, [2]int{p[0], min(p[1], a)})
			}
			if b < p[1] {
				left = append(left, [2]int{max(p[0], b), p[1]})
			}
		}
		parts = left
	}
	return parts
}

// perches: the visible stretches of window tops she can sit on: wide enough, with room above
// them for her bubble. (Only x, y and width matter.)
func (p *Pet) perches() []rect {
	var out []rect
	for i, w := range p.windows {
		for _, part := range visible(w.x, w.x+w.w, p.windows[i+1:], func(o rect) (int, int, bool) {
			return o.x, o.x + o.w, o.y <= w.y && w.y < o.y+o.h
		}) {
			mid := (part[0] + part[1]) / 2
			if a := workArea(mid, w.y); part[1]-part[0] >= 160 && a.contains(mid, w.y) && w.y-a.y > bubbleRoom {
				out = append(out, rect{part[0], w.y, part[1] - part[0], 0})
			}
		}
	}
	return out
}

// floors: what she can stand on to scratch: window tops she could perch on, and the bottoms of
// the screens, where no other screen carries on below. (Only x, y and width matter.)
func (p *Pet) floors() []rect {
	out := p.perches()
	for _, a := range workAreas {
		bottom := a.y + a.h
		for _, part := range visible(a.x, a.x+a.w, workAreas, func(b rect) (int, int, bool) {
			return b.x, b.x + b.w, b.y <= bottom && bottom < b.y+b.h
		}) {
			if part[1]-part[0] >= 160 {
				out = append(out, rect{part[0], bottom, part[1] - part[0], 0})
			}
		}
	}
	return out
}

// An edge is a window's side she can rub against: her cheek goes on x, anywhere from y0 to y1,
// and she faces it (1: it's on her right, -1: on her left).
type edge struct{ x, y0, y1, facing int }

// sides: the visible stretches of windows' sides with room beside them for her, and for her
// bubble above; and the screens' own sides, where no other screen carries on.
func (p *Pet) sides() []edge {
	var out []edge
	cat, cheek := p.win.w, int(p.sheet.RubCheek[1]*scale)
	add := func(a rect, x int, part [2]int, facing int) { // a: the screen she stands on
		y0 := max(part[0]+20, a.y+bubbleRoom+cheek)
		y1 := min(part[1]-20, a.y+a.h-(cat-cheek))
		if y1 > y0 {
			out = append(out, edge{x, y0, y1, facing})
		}
	}
	for _, a := range workAreas {
		for _, facing := range []int{1, -1} { // its right side on her right, its left side on her left
			x, beyond := a.x+a.w, a.x+a.w // where her cheek goes, and the pixels just past it
			if facing < 0 {
				x, beyond = a.x, a.x-1
			}
			for _, part := range visible(a.y, a.y+a.h, workAreas, func(b rect) (int, int, bool) {
				return b.y, b.y + b.h, b.x <= beyond && beyond < b.x+b.w
			}) {
				add(a, x, part, facing)
			}
		}
	}
	for i, w := range p.windows {
		for _, facing := range []int{1, -1} { // a left side she rubs from the left, a right side from the right
			x, column := w.x, w.x // where her cheek goes, and the window's pixels along that side
			if facing < 0 {
				x, column = w.x+w.w, w.x+w.w-1
			}
			a := workArea(x-facing, w.y+w.h/2)
			if !a.contains(x-facing*cat, a.y+a.h/2) {
				continue // no room beside it
			}
			for _, part := range visible(w.y, w.y+w.h, p.windows[i+1:], func(o rect) (int, int, bool) {
				return o.y, o.y + o.h, o.x <= column && column < o.x+o.w
			}) {
				add(a, x, part, facing)
			}
		}
	}
	return out
}

// walkTo takes one step towards (tx, ty). True once she's there.
func (p *Pet) walkTo(tx, ty float64) bool {
	dx, dy := tx-p.x, ty-p.y
	dist := math.Hypot(dx, dy)
	if dist <= speed {
		p.x, p.y = tx, ty
	} else {
		sector := (int(math.Round(math.Atan2(dy, dx)/(math.Pi/4)))%8 + 8) % 8 // 0 = right, then clockwise
		p.walkView = walkViews[sector]
		if walkFacing[sector] != 0 {
			p.facingRight = walkFacing[sector] > 0
		}
		p.x += dx / dist * speed
		p.y += dy / dist * speed
	}
	p.moveCat()
	return dist <= speed
}

// here: the usable part of the screen she's on now.
func (p *Pet) here() rect { return p.screenOf(p.x, p.y) }

// screenOf: the usable part of the screen she'd be on with her sprite's top left at (x, y).
func (p *Pet) screenOf(x, y float64) rect { return workArea(int(x)+p.win.w/2, int(y)+p.win.w/2) }

// clamp keeps a spot inside a screen's usable area g, with room above her for the bubble.
func (p *Pet) clamp(g rect, x, y float64) [2]float64 {
	cat := float64(p.win.w)
	return [2]float64{max(float64(g.x), min(x, float64(g.x+g.w)-cat)),
		max(float64(g.y+bubbleRoom), min(y, float64(g.y+g.h)-cat))}
}

// walkVia takes one step towards (tx, ty), going from screen to screen through the edges they
// share so she never walks off them. True once she's there.
func (p *Pet) walkVia(tx, ty float64) bool {
	if g := p.screenOf(tx, ty); len(p.route) == 0 || g != p.routeTo { // somewhere else now: plan again
		p.route, _ = p.crossings(g)
		p.routeTo = g
	}
	if len(p.route) > 0 {
		if p.walkTo(p.route[0][0], p.route[0][1]) {
			p.route = p.route[1:]
		}
		return false
	}
	return p.walkTo(tx, ty) // on its screen (or it's out of reach that way: straight there)
}

// randomSpot: somewhere to go, every spot she can reach equally likely. A screen that can't be
// reached without leaving the screens (it shares no edge with the others) is skipped.
func (p *Pet) randomSpot() [2]float64 {
	room := func(g rect) int { return (g.w - p.win.w) * (g.h - bubbleRoom - p.win.w) }
	total := 0
	for _, g := range workAreas {
		total += room(g)
	}
	for total > 0 {
		n := rand.Intn(total)
		for _, g := range workAreas {
			if n -= room(g); n < 0 {
				if _, ok := p.crossings(g); ok {
					return p.spotIn(g)
				}
				break
			}
		}
	}
	return p.spotIn(p.here())
}

// crossings: the spots she steps through to get from her screen to screen g, two at each edge
// on the way, through any screens in between. Not ok if there's no such way.
func (p *Pet) crossings(g rect) ([][2]float64, bool) {
	seen := map[rect]bool{}
	var from func(s rect, x, y float64) ([][2]float64, bool)
	from = func(s rect, x, y float64) ([][2]float64, bool) {
		if s == g {
			return nil, true
		}
		seen[s] = true
		for _, next := range workAreas {
			if out, in, ok := p.crossing(s, next, x, y); ok && !seen[next] {
				if rest, ok := from(next, in[0], in[1]); ok {
					return append([][2]float64{out, in}, rest...), true
				}
			}
		}
		return nil, false
	}
	return from(p.here(), p.x, p.y)
}

// spotIn: a random spot for her on screen g, with room above her for the bubble.
func (p *Pet) spotIn(g rect) [2]float64 {
	return [2]float64{float64(randInt(g.x, g.x+g.w-p.win.w)), float64(randInt(g.y+bubbleRoom, g.y+g.h-p.win.w))}
}

// crossing: where she steps from screen a onto screen b, on either side of the edge they share,
// as level as it can be with (x, y), where she's coming from. Not ok if they share no edge wide
// enough for her.
func (p *Pet) crossing(a, b rect, x, y float64) (from, to [2]float64, ok bool) {
	cat := float64(p.win.w)
	switch {
	case a.x+a.w == b.x || b.x+b.w == a.x: // side by side
		lo, hi := float64(max(a.y, b.y)+bubbleRoom), float64(min(a.y+a.h, b.y+b.h))-cat
		if lo > hi {
			return
		}
		x, y = float64(max(a.x, b.x)), max(lo, min(y, hi))
	case a.y+a.h == b.y || b.y+b.h == a.y: // one above the other
		lo, hi := float64(max(a.x, b.x)), float64(min(a.x+a.w, b.x+b.w))-cat
		if lo > hi {
			return
		}
		x, y = max(lo, min(x, hi)), float64(max(a.y, b.y))
	default:
		return
	}
	return p.clamp(a, x, y), p.clamp(b, x, y), true
}

// ---- drawing ----

func (p *Pet) moveCat() { p.win.move(int(p.x), int(p.y)-headroom) }

func (p *Pet) raise() {
	p.win.raise()
	p.bubble.raise()
}

func (p *Pet) facing() bool {
	return p.facingRight || slices.ContainsFunc(frontFacing, func(f string) bool { return strings.HasPrefix(p.frame, f) })
}

// render redraws her when her frame changes (or hearts are floating up), and her bubble.
func (p *Pet) render() {
	key := fmt.Sprint(p.frame, p.facing())
	if key != p.drawn || len(p.hearts) > 0 {
		sprite := p.sheet.get(p.frame)
		clear(p.win.img)
		blit(p.win.img, sprite, 0, headroom, scale, !p.facing(), 1)
		for _, h := range p.hearts { // little hearts floating up while you pet her
			if age := p.tick - h.born; age >= 0 {
				blit(p.win.img, p.sheet.get("heart"), p.win.w/2+int(h.dx), headroom+8*scale-age*2, scale, false,
					1-float64(age)/30)
			}
		}
		p.win.flush()
		if key != p.drawn {
			// Only she (plus a small margin, so she notices the pointer coming) catches the mouse.
			if p.shapes[key] == nil {
				p.shapes[key] = grown(shapeOf(sprite, 0, headroom, scale, !p.facing()), 10)
			}
			p.win.setShape(p.shapes[key])
		}
		p.drawn = key
	}
	p.renderBubble()
}

// renderBubble: the newest message, in a speech bubble above her head.
func (p *Pet) renderBubble() {
	if len(p.pending) == 0 {
		p.bubble.hide()
		p.bubbleKey = ""
		return
	}
	info := p.pending[len(p.pending)-1]
	key := fmt.Sprint(info.session, info.text, p.expanded, len(p.pending))
	if key != p.bubbleKey {
		p.bubbleKey = key
		var text string
		if p.expanded {
			if info.detail == nil {
				d := lastReply(info.transcript)
				info.detail = &d
			}
			detail := *info.detail
			if detail == "" {
				detail = "(nothing more to show)"
			}
			text = fmt.Sprintf("%s: %s\n\n%s", info.project, info.text, detail)
		} else {
			text = fmt.Sprintf("%s: %s", info.project, info.text)
			if r := []rune(info.text); len(r) >= 140 {
				text = fmt.Sprintf("%s: %s...", info.project, string(r[:137]))
			}
			if len(p.pending) > 1 {
				text += fmt.Sprintf("\n(+%d more)", len(p.pending)-1)
			}
		}
		lines := wrap(text, 436)
		for len(lines)*lineHeight() > 300 { // too long to show: trim until it fits
			r := []rune(text)
			text = string(r[:len(r)*9/10]) + "…"
			lines = wrap(text, 436)
		}
		width := 0
		for _, line := range lines {
			width = max(width, textWidth(line))
		}
		w, h := width+20, len(lines)*lineHeight()+14
		p.bubble.resize(w, h+11)
		clear(p.bubble.img)
		box(p.bubble.img, 0, 0, w, h, accent) // terminal-style box: dark glass, amber hairline and text
		tail(p.bubble.img, w/2, h-1, accent)
		drawText(p.bubble.img, lines, 10, 7, accent)
		p.bubble.flush()
	}
	p.bubble.move(int(p.x)+p.win.w/2-p.bubble.w/2, int(p.y)-10-(p.bubble.h-11))
	if !p.bubble.shown {
		p.bubble.show()
		p.bubble.raise()
	}
}

// ---- mouse ----

func (p *Pet) onBubble(button, _, _, _, _ int) {
	switch button {
	case 1: // jump to the session that called
		if info := p.popPending(); info != nil {
			go focus(info)
		}
		p.expanded = false
	case 3: // read more of it
		p.expanded = !p.expanded
	}
	p.renderBubble()
}

func (p *Pet) onPress(button, rx, ry, _, _ int) {
	switch button {
	case 3: // a treat (quitting lives in the tray icon)
		p.treat()
	case 1:
		p.press = &[2]int{rx, ry}
	}
}

func (p *Pet) onMotion(rx, ry, _, _ int) {
	if p.press != nil && abs(rx-p.press[0])+abs(ry-p.press[1]) > 6 {
		p.dragging = true
	}
	if p.dragging { // hold her by the belly: that pixel of the sprite stays under the pointer
		p.x, p.y = p.place(p.sheet.HangGrab, float64(rx), float64(ry), p.facingRight)
		p.moveCat()
		p.route, p.targetSpot = nil, [2]float64{p.x, p.y} // walking resumes from wherever she's dropped
	} else if p.press == nil { // the pointer is near her: she rubs against it
		p.hover = true
		p.pointerX = rx // she turns to it only once she's actually rubbing (see step)
	}
}

func (p *Pet) onRelease(button, _, _ int) {
	if button != 1 || p.press == nil {
		return
	}
	wasDrag := p.dragging
	p.press, p.dragging = nil, false
	if wasDrag {
		return
	}
	// A click is petting. It also means you've seen her message.
	p.petUntil = mono() + 2.5
	for k := range 3 {
		p.hearts = append(p.hearts, heart{p.tick + k*6, uniform(-16, 4)})
	}
	p.popPending()
	p.expanded = false
}
