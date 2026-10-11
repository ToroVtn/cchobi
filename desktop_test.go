package main

import (
	"slices"
	"testing"
)

// A cut-down `swaymsg -r -t get_tree`: a tiled terminal and a tiled VS Code on one output, a
// hidden tab, and a floating window above them.
const swayTree = `{"id":1,"type":"root","nodes":[{"id":2,"type":"output","name":"DP-1","nodes":[
 {"id":3,"type":"workspace","name":"1","nodes":[
  {"id":4,"type":"con","name":"~/cchobi — fish","app_id":"foot","visible":true,
   "rect":{"x":0,"y":30,"width":960,"height":1050},"deco_rect":{"x":0,"y":0,"width":960,"height":20}},
  {"id":5,"type":"con","layout":"tabbed","nodes":[
   {"id":6,"type":"con","name":"main.go - cchobi - Visual Studio Code","app_id":"code","visible":true,
    "rect":{"x":960,"y":30,"width":960,"height":1050}},
   {"id":7,"type":"con","name":"hidden tab","app_id":"foot","visible":false,
    "rect":{"x":960,"y":30,"width":960,"height":1050}}]}],
  "floating_nodes":[{"id":8,"type":"floating_con","name":"calc","visible":true,"window_properties":{"class":"Xcalc"},
   "rect":{"x":100,"y":100,"width":300,"height":200}}]}]}]}`

const swayWorkspaces = `[{"name":"1","visible":true,"rect":{"x":0,"y":30,"width":1920,"height":1050}},
 {"name":"2","visible":false,"rect":{"x":0,"y":30,"width":1920,"height":1050}}]`

func TestSway(t *testing.T) {
	want := []rect{{0, 10, 960, 1070}, {960, 30, 960, 1050}, {100, 100, 300, 200}} // floating last: on top
	if got := swayWindows([]byte(swayTree)); !slices.Equal(got, want) {
		t.Errorf("windows: got %v, want %v", got, want)
	}
	if got := swayAreas([]byte(swayWorkspaces)); !slices.Equal(got, []rect{{0, 30, 1920, 1050}}) {
		t.Errorf("areas: got %v", got)
	}
	if ids := len(swayViews([]byte(swayTree))); ids != 4 {
		t.Errorf("views: got %d, want 4 (hidden tab included, for focusing)", ids)
	}
}

func TestPickWindow(t *testing.T) {
	titles := []string{"other", "~/cchobi — fish", "main.go - cchobi - Visual Studio Code"}
	classes := []string{"foot", "foot", "code"}
	for _, c := range []struct {
		project string
		vscode  bool
		want    int
	}{{"cchobi", true, 2}, {"cchobi", false, 1}, {"other", true, 0}, {"nope", false, -1}} {
		if got := pickWindow(titles, classes, c.project, c.vscode); got != c.want {
			t.Errorf("pickWindow(%q, vscode=%v) = %d, want %d", c.project, c.vscode, got, c.want)
		}
	}
}
