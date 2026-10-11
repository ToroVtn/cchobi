package main

// The desktop, as sway tells it over swaymsg: where the windows are, and focusing one.

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type swayNode struct {
	ID               int
	Name             string // the title
	Type             string
	Rect             swayRect
	DecoRect         swayRect `json:"deco_rect"` // its title bar, which rect leaves out
	Visible          bool
	AppID            string                 `json:"app_id"`
	WindowProperties struct{ Class string } `json:"window_properties"` // an X11 one's (through XWayland)
	Nodes            []swayNode
	FloatingNodes    []swayNode `json:"floating_nodes"`
}

type swayRect struct{ X, Y, Width, Height int }

func swaymsg(args ...string) []byte {
	out, _ := exec.Command("swaymsg", append([]string{"-r"}, args...)...).Output()
	return out
}

// swayViews: every window in sway's tree, tiled ones first, then the floating ones above them.
func swayViews(tree []byte) []swayNode {
	var root swayNode
	json.Unmarshal(tree, &root)
	var tiled, floating []swayNode
	var walk func(n swayNode, float bool)
	walk = func(n swayNode, float bool) {
		if (n.Type == "con" || n.Type == "floating_con") && len(n.Nodes) == 0 && len(n.FloatingNodes) == 0 {
			if float {
				floating = append(floating, n)
			} else {
				tiled = append(tiled, n)
			}
		}
		for _, c := range n.Nodes {
			walk(c, float)
		}
		for _, c := range n.FloatingNodes {
			walk(c, true)
		}
	}
	walk(root, false)
	return append(tiled, floating...)
}

// swayLayout: the windows on screen (tiled ones don't overlap, so the order only matters for
// floating ones), and each output's usable area: its visible workspace, which leaves out the bar.
func swayLayout() (windows, areas []rect) {
	return swayWindows(swaymsg("-t", "get_tree")), swayAreas(swaymsg("-t", "get_workspaces"))
}

func swayWindows(tree []byte) (out []rect) {
	for _, v := range swayViews(tree) {
		if v.Visible {
			d := v.DecoRect.Height
			out = append(out, rect{v.Rect.X, v.Rect.Y - d, v.Rect.Width, v.Rect.Height + d})
		}
	}
	return out
}

func swayAreas(workspaces []byte) (out []rect) {
	var wss []struct {
		Visible bool
		Rect    swayRect
	}
	json.Unmarshal(workspaces, &wss)
	for _, w := range wss {
		if w.Visible {
			out = append(out, rect{w.Rect.X, w.Rect.Y, w.Rect.Width, w.Rect.Height})
		}
	}
	return out
}

func swayFocus(project string, vscode bool) bool {
	views := swayViews(swaymsg("-t", "get_tree"))
	titles, classes := make([]string, len(views)), make([]string, len(views))
	for i, v := range views {
		titles[i], classes[i] = v.Name, v.AppID+" "+v.WindowProperties.Class
	}
	i := pickWindow(titles, classes, project, vscode)
	return i >= 0 && exec.Command("swaymsg", fmt.Sprintf("[con_id=%d]", views[i].ID), "focus").Run() == nil
}
