package main

import (
	"encoding/json"
	"testing"
)

func TestGarden(t *testing.T) {
	const h = 3600.0
	dryOrder := []int{4, 5, 6, 7, 1, 2, 3, 0}
	fallTime = [2]float64{9 * h, 9 * h} // fixed, so leaves hang on until the checks below
	jitter = 0                          // and checks exactly every 4 h, to step through them
	check := func(ok bool, what string) {
		t.Helper()
		if !ok {
			t.Fatal(what)
		}
	}
	g := newGarden(nil, 8, dryOrder, 0)
	check(len(g.update(4*h-1)) == 0 && g.count("dry") == 0, "dried before the first check")
	g.update(4 * h) // first check, never watered: a leaf dries
	check(len(g.dry()) == 1, "first check")
	g.water(6.5 * h) // watered 1.5 h before the next check: nothing dries
	g.update(8 * h)
	check(len(g.dry()) == 1, "watered in time")
	g.update(12 * h) // 5.5 h since watering at this check: another dries
	check(len(g.dry()) == 2 && g.count("green") == 6, "second leaf")
	var first []int
	for k, leaf := range g.Leaves {
		if leaf.Falls == 4*h+fallTime[0] {
			first = append(first, k)
		}
	}
	g.water(13 * h)          // keep it watered from here on
	fell := g.update(13 * h) // the first leaf (dry since 4 h) falls at 13 h
	check(len(fell) == 1 && fell[0] == first[0] && g.count("gone") == 1, "falls")
	g.water(15.5 * h)
	g.update(16 * h) // well watered: the lost leaf grows back
	check(g.count("gone") == 0 && g.count("green") == 7, "regrows")
	regrown := first[0]
	for _, at := range []float64{20, 24, 28, 32, 36, 40} { // neglected: the regrown leaf, the newest, dries last
		g.update(at * h)
		if g.Leaves[regrown].State != "green" {
			check(g.count("green") == 0, "the newest leaf dried while older ones were still green")
		}
	}
	data, _ := json.Marshal(g) // survives a save and load
	var saved Garden
	json.Unmarshal(data, &saved)
	g2 := newGarden(&saved, 8, dryOrder, 0)
	check(g2.NextCheck == g.NextCheck && json.Valid(data) && g2.count("dry") == g.count("dry"), "save and load")
	g3 := newGarden(nil, 8, dryOrder, 0)
	g3.update(400 * h) // left alone for ages: everything dries and drops
	check(g3.count("green") == 0, "neglect")
	g4 := newGarden(nil, 8, dryOrder, 0)
	g4.water(1 * h)
	w, c := g4.describe(1.5 * h)
	check(w == "30 min ago" && c == "in about 3 h", "describe 2h30") // 2 h 30 min to the check
	_, c = g4.describe(1.5*h + 60)
	check(c == "in about 2 h", "describe 2h29")
	_, c = g4.describe(0.5*h + 60)
	check(c == "in about 3 h", "describe 3h29")
	w, c = g4.describe(3.6 * h)
	check(w == "2 h 36 min ago" && c == "in a few minutes", "describe soon")
	g5 := newGarden(nil, 8, dryOrder, 0)
	g5.water(5 * h)
	g5.update(8 * h) // watered 3 h before: too long ago now, a leaf dries
	check(len(g5.dry()) == 1, "thirsty window")
	jitter = 30 * 60
	g6 := newGarden(nil, 8, dryOrder, 0)
	lo, hi := 1e18, 0.0
	for range 200 { // checks wander between 3.5 h and 4.5 h apart
		last := g6.NextCheck
		g6.update(g6.NextCheck)
		gap := g6.NextCheck - last
		lo, hi = min(lo, gap), max(hi, gap)
	}
	check(lo >= 3.5*h && hi <= 4.5*h && hi-lo > 0.5*h, "jitter")
}
