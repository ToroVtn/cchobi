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
		to := p.randomSpot()
		route, _ := p.crossings(p.screenOf(to[0], to[1]))
		for _, spot := range append(route, to) { // she walks a straight line to each spot
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
	p := &Pet{win: &Win{w: 96}, sheet: &Sheet{Size: 48, RubCheek: [2]float64{44, 26}}}
	p.windows = []rect{ // bottom to top
		{0, 0, 2560, 1390},     // maximized, at the bottom: no room beside it
		{500, 300, 800, 600},   // room on both sides
		{2000, 1400, 400, 300}, // below the screen's usable area: its sides are out of reach
	}
	sides := p.sides()
	if len(sides) != 4 {
		t.Fatalf("want the screen's two sides and the middle window's two, got %v", sides)
	}
	p.windows = append(p.windows, rect{1200, 0, 600, 700}) // on top, hiding the upper part of its right side
	if right := p.sides()[3]; right.x != 1300 || right.y0 < 700 {
		t.Fatalf("she'd rub the hidden part of a side: %v", right)
	}
	if perches := p.perches(); len(perches) != 1 || perches[0].x != 500 || perches[0].w != 700 {
		t.Fatalf("want the visible part of the middle window's top, got %v", perches)
	}
	past := float64((48 - 44) * scale) // what of her is past her cheek
	for _, e := range sides {
		x, y := p.place(p.sheet.RubCheek, float64(e.x), float64(e.y1), e.facing > 0)
		if x < -past || x+96 > 2560+past || y < bubbleRoom || y+96 > 1390 {
			t.Fatalf("she'd stick out of the screen at %v, %v rubbing %v", x, y, e)
		}
		if (e.facing > 0) != (e.x == 500 || e.x == 2560) { // a side on her right she rubs from its left
			t.Fatalf("facing the wrong way: %v", e)
		}
	}
	workAreas = []rect{{0, 0, 1920, 1040}, {1920, 400, 2560, 1400}} // the right one lower
	p.windows = nil
	sides = p.sides() // the outer sides, and the parts of the middle one only one screen has
	if len(sides) != 4 || sides[0] != (edge{1920, 182, 380, 1}) || sides[3].x != 1920 || sides[3].y0 < 1040 {
		t.Fatalf("want the screens' sides where no other screen carries on, got %v", sides)
	}
}

func TestFloors(t *testing.T) {
	workAreas = []rect{{0, 0, 1920, 1040}, {0, 1040, 1920, 1040}, {1920, 0, 1280, 1040}} // two stacked, one beside
	p := &Pet{windows: []rect{{100, 1500, 800, 400}}}
	floors := p.floors() // the window's top, and the bottoms with no screen below
	if len(floors) != 3 || floors[0].y != 1500 || floors[1].y != 2080 || floors[2] != (rect{1920, 1040, 1280, 0}) {
		t.Fatalf("want the window's top and the two bottom screen edges, got %v", floors)
	}
}
