package main

// Her jade plant's life, without any drawing: leaves dry out unless it's watered often enough,
// dry leaves fall off after a while, and fallen ones lie around (or in the pot) until she eats
// them or you throw them away. garden_test.go checks the rules (go test).
//
// About every cycle (give or take jitter, so you can't time it) the plant checks on itself:
//   - not watered in the thirsty hours before the check -> a green leaf dries out (the one that's
//     been green the longest; if several tie, one of them at random)
//   - watered in that window                            -> one lost leaf grows back
// So one watering doesn't fix it: water it at least every 2 hours to keep it healthy.

import (
	"fmt"
	"math/rand"
)

var (
	cycle    = 4 * 3600.0
	jitter   = 30 * 60.0 // each check comes cycle +- this after the last one (random)
	thirsty  = 2 * 3600.0
	fallTime = [2]float64{3600, 4 * 3600} // a dry leaf drops off this long after drying (random)
)

type Leaf struct {
	State string  `json:"state"` // green, dry or gone
	Since float64 `json:"since,omitempty"`
	Falls float64 `json:"falls,omitempty"`
}

type Garden struct {
	Leaves    []Leaf   `json:"leaves"`
	Watered   float64  `json:"watered"`
	NextCheck float64  `json:"next_check"`
	Pot       int      `json:"pot"`    // dry leaves lying in the pot
	Fallen    [][2]int `json:"fallen"` // screen positions of dry leaves on the "floor"
	dryOrder  []int    // which leaves go brown first
}

func interval() float64 { return cycle + (rand.Float64()*2-1)*jitter }

// newGarden: a healthy plant, or the saved one if there is one.
func newGarden(saved *Garden, leaves int, dryOrder []int, now float64) *Garden {
	g := saved
	if g == nil || len(g.Leaves) != leaves {
		g = &Garden{Watered: now, NextCheck: now + interval()}
		for range leaves {
			g.Leaves = append(g.Leaves, Leaf{State: "green", Since: now})
		}
	}
	g.dryOrder = dryOrder
	return g
}

func (g *Garden) water(now float64) { g.Watered = now }

// update catches up with the time that's passed (also while the pet wasn't running).
// Returns the indexes of leaves that have just fallen off.
func (g *Garden) update(now float64) []int {
	for now >= g.NextCheck {
		t := g.NextCheck
		var green, gone []int
		for k, leaf := range g.Leaves {
			if leaf.State == "green" {
				green = append(green, k)
			}
		}
		for i := len(g.dryOrder) - 1; i >= 0; i-- {
			if k := g.dryOrder[i]; g.Leaves[k].State == "gone" {
				gone = append(gone, k)
			}
		}
		if t-g.Watered > thirsty { // thirsty: the oldest green leaf dries
			if len(green) > 0 {
				oldest := g.Leaves[green[0]].Since
				for _, k := range green {
					oldest = min(oldest, g.Leaves[k].Since)
				}
				var ties []int
				for _, k := range green {
					if g.Leaves[k].Since == oldest {
						ties = append(ties, k)
					}
				}
				k := ties[rand.Intn(len(ties))]
				g.Leaves[k] = Leaf{State: "dry", Falls: t + fallTime[0] + rand.Float64()*(fallTime[1]-fallTime[0])}
			}
		} else if len(gone) > 0 { // looked after: a lost leaf grows back
			g.Leaves[gone[0]] = Leaf{State: "green", Since: t}
		}
		g.NextCheck += interval()
	}
	var fell []int
	for k, leaf := range g.Leaves {
		if leaf.State == "dry" && now >= leaf.Falls {
			g.Leaves[k] = Leaf{State: "gone"}
			fell = append(fell, k)
		}
	}
	return fell
}

func (g *Garden) count(state string) int {
	n := 0
	for _, leaf := range g.Leaves {
		if leaf.State == state {
			n++
		}
	}
	return n
}

// dry: dry leaves still on the plant, the ones she'd reach first first.
func (g *Garden) dry() []int {
	var out []int
	for _, k := range g.dryOrder {
		if g.Leaves[k].State == "dry" {
			out = append(out, k)
		}
	}
	return out
}

// pick: a dry leaf leaves the plant (eaten, pulled off, or thrown away).
func (g *Garden) pick(k int) { g.Leaves[k] = Leaf{State: "gone"} }

// describe: how long ago it was watered, and roughly when it next checks on itself, for the tray.
func (g *Garden) describe(now float64) (string, string) {
	m := int((now - g.Watered) / 60)
	watered := fmt.Sprintf("%d min ago", m)
	if m >= 60 {
		watered = fmt.Sprintf("%d h %d min ago", m/60, m%60)
	}
	hours := int((g.NextCheck-now)/3600 + 0.5) // 3 h 29 min -> about 3 h; 2 h 29 min -> about 2 h
	check := "in a few minutes"
	if hours > 0 {
		check = fmt.Sprintf("in about %d h", hours)
	}
	return watered, check
}
