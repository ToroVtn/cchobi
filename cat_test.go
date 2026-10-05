package main

import "testing"

func TestWanderAcrossScreens(t *testing.T) {
	workAreas = []rect{ // the middle one lower: nothing above it
		{0, 0, 1920, 1040}, {1920, 400, 2560, 1400}, {4480, 0, 1920, 1040}}
	p := &Pet{win: &Win{w: 96}}
	onScreen := func(x, y float64) bool {
		for _, a := range workAreas {
			if a.contains(int(x), int(y)) {
				return true
			}
		}
		return false
	}
	p.x, p.y = 100, 150 // top left of the left screen: a straight line to the right one would leave the screens
	crossed, across := 0, 0
	for range 200 {
		start := p.here()
		for _, spot := range p.wander() { // she walks a straight line to each spot
			for i := range 101 {
				f := float64(i) / 100
				x, y := p.x+(spot[0]-p.x)*f, p.y+(spot[1]-p.y)*f
				if !onScreen(x, y) || !onScreen(x+95, y+95) {
					t.Fatalf("she'd walk off the screens at %v, %v on her way to %v", x, y, spot)
				}
			}
			p.x, p.y = spot[0], spot[1]
		}
		if p.here() != start {
			crossed++
		}
		if start == workAreas[0] && p.here() == workAreas[2] {
			across++ // through the middle one, in one walk
		}
	}
	if crossed == 0 || across == 0 {
		t.Fatalf("she changed screens %d times, %d of them from one end to the other", crossed, across)
	}
}

func TestSides(t *testing.T) {
	workAreas = []rect{{0, 0, 2560, 1390}} // a screen with a 50 px panel at the bottom
	p := &Pet{win: &Win{w: 96}, sheet: &Sheet{RubCheek: [2]float64{44, 26}}}
	p.windows = []rect{ // bottom to top
		{0, 0, 2560, 1390},     // maximized, at the bottom: no room beside it
		{500, 300, 800, 600},   // room on both sides
		{2000, 1400, 400, 300}, // below the screen's usable area: its sides are out of reach
	}
	sides := p.sides()
	if len(sides) != 2 {
		t.Fatalf("want the two sides of the middle window, got %v", sides)
	}
	p.windows = append(p.windows, rect{1200, 0, 600, 700}) // on top, hiding the upper part of its right side
	if right := p.sides()[1]; right.x != 1300 || right.y0 < 700 {
		t.Fatalf("she'd rub the hidden part of a side: %v", right)
	}
	if perches := p.perches(); len(perches) != 1 || perches[0].x != 500 || perches[0].w != 700 {
		t.Fatalf("want the visible part of the middle window's top, got %v", perches)
	}
	for _, e := range sides {
		x, y := p.place(p.sheet.RubCheek, float64(e.x), float64(e.y1), e.facing > 0)
		if x < 0 || x+96 > 2560 || y < bubbleRoom || y+96 > 1390 {
			t.Fatalf("she'd stick out of the screen at %v, %v rubbing %v", x, y, e)
		}
		if (e.facing > 0) != (e.x == 500) { // left side from the left, right side from the right
			t.Fatalf("facing the wrong way: %v", e)
		}
	}
}
