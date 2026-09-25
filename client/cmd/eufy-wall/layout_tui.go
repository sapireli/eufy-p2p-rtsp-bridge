package main

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"eufy-wall/internal/config"
)

type screenKey struct {
	name string
	r    rune
}

type cameraPicker struct {
	kind    string
	serials []string
	index   int
	all     bool
	checked map[string]bool
}

type layoutScreen struct {
	editor   *layoutEditor
	selected int
	step     int
	resize   bool
	width    int
	height   int
	status   string
	help     bool
	prompt   string
	input    string
	picker   *cameraPicker
	saved    []byte
	apply    func() error
}

func (s *layoutScreen) tile() (*config.Tile, error) {
	c, err := s.editor.config()
	if err != nil {
		return nil, err
	}
	if len(c.Tiles) == 0 {
		return nil, errors.New("add a tile first")
	}
	if s.selected >= len(c.Tiles) {
		s.selected = len(c.Tiles) - 1
	}
	return &c.Tiles[s.selected], nil
}

func (s *layoutScreen) edit(args ...string) error {
	if err := s.editor.change(func(c *config.Config) error { return editorMutation(c, args) }); err != nil {
		return err
	}
	s.status = "validated: " + strings.Join(args, " ")
	return nil
}

func (s *layoutScreen) handle(key screenKey) (bool, error) {
	if s.picker != nil {
		return false, s.handlePicker(key)
	}
	if s.prompt != "" {
		return s.handlePrompt(key)
	}
	if key.name == "interrupt" {
		key.r = 'q'
	}
	if key.name == "tab" || key.r == 'n' {
		return false, s.selectOffset(1)
	}
	if key.name == "backtab" || key.r == 'p' {
		return false, s.selectOffset(-1)
	}
	if key.name == "up" || key.name == "down" || key.name == "left" || key.name == "right" {
		t, err := s.tile()
		if err != nil {
			return false, err
		}
		dx, dy := 0, 0
		switch key.name {
		case "up":
			dy = -s.step
		case "down":
			dy = s.step
		case "left":
			dx = -s.step
		case "right":
			dx = s.step
		}
		verb := "move"
		if s.resize {
			verb = "resize"
		}
		return false, s.edit(verb, t.ID, strconv.Itoa(dx), strconv.Itoa(dy))
	}
	switch key.r {
	case '1', '4', '8':
		s.step = int(key.r - '0')
		s.status = fmt.Sprintf("step: %d grid cell(s)", s.step)
	case 'r':
		s.resize = !s.resize
	case 'u':
		if !s.editor.undo() {
			return false, errors.New("nothing to undo")
		}
		s.status = "undo"
	case 'y':
		if !s.editor.redo() {
			return false, errors.New("nothing to redo")
		}
		s.status = "redo"
	case '?':
		s.help = !s.help
	case 'q':
		if bytes.Equal(s.saved, s.editor.history[s.editor.at]) {
			return true, nil
		}
		s.prompt = "quit"
	case 'e', 'c', 'w', 'd':
		t, err := s.tile()
		if err != nil {
			return false, err
		}
		if key.r == 'c' && s.editor.inventory != nil {
			return false, s.openPicker("camera", t)
		}
		if key.r == 'w' && s.editor.inventory != nil {
			return false, s.openPicker("watch", t)
		}
		switch key.r {
		case 'e':
			s.prompt = "rect"
			s.input = fmt.Sprintf("%d %d %d %d", t.Rect.X, t.Rect.Y, t.Rect.W, t.Rect.H)
		case 'c':
			s.prompt = "camera"
		case 'w':
			s.prompt = "watch"
		case 'd':
			s.prompt = "delete"
		}
	case 's':
		s.prompt, s.input = "save", "eufy-wall.draft.yaml"
	case 'A':
		s.prompt = "apply"
	case 'i':
		s.prompt = "inventory"
	case 'o':
		s.prompt = "output"
	case 't':
		s.prompt = "template"
	case 'a':
		s.prompt = "add"
	case 'P':
		s.prompt, s.input = "png", "eufy-wall-preview.png"
	}
	return false, nil
}

func (s *layoutScreen) selectOffset(offset int) error {
	c, err := s.editor.config()
	if err != nil {
		return err
	}
	if len(c.Tiles) == 0 {
		return errors.New("add a tile first")
	}
	s.selected = (s.selected + offset + len(c.Tiles)) % len(c.Tiles)
	return nil
}

func (s *layoutScreen) openPicker(kind string, tile *config.Tile) error {
	p := &cameraPicker{kind: kind, checked: map[string]bool{}, all: len(tile.Watch) == 0}
	for sn := range s.editor.inventory {
		p.serials = append(p.serials, sn)
	}
	sort.Strings(p.serials)
	for _, sn := range tile.Watch {
		p.checked[sn] = true
	}
	if kind == "camera" {
		for i, sn := range p.serials {
			if sn == tile.Camera {
				p.index = i
				break
			}
		}
	}
	s.picker = p
	return nil
}

func (s *layoutScreen) handlePicker(key screenKey) error {
	p := s.picker
	if key.name == "cancel" || key.name == "escape" {
		s.picker = nil
		return nil
	}
	if key.name == "up" {
		if p.index > 0 {
			p.index--
		}
		return nil
	}
	if key.name == "down" {
		if p.index+1 < len(p.serials) {
			p.index++
		}
		return nil
	}
	if key.r == 'a' && p.kind == "watch" {
		p.all = true
		p.checked = map[string]bool{}
		return nil
	}
	if key.r == ' ' && p.kind == "watch" && len(p.serials) > 0 {
		p.all = false
		sn := p.serials[p.index]
		p.checked[sn] = !p.checked[sn]
		return nil
	}
	if key.name != "enter" {
		return nil
	}
	t, err := s.tile()
	if err != nil {
		return err
	}
	if p.kind == "camera" {
		if len(p.serials) == 0 {
			return errors.New("camera inventory is empty")
		}
		err = s.edit("camera", t.ID, p.serials[p.index])
	} else {
		watch := "all"
		if !p.all {
			selected := make([]string, 0, len(p.checked))
			for _, sn := range p.serials {
				if p.checked[sn] {
					selected = append(selected, sn)
				}
			}
			if len(selected) == 0 {
				return errors.New("select at least one camera or press a for all")
			}
			watch = strings.Join(selected, ",")
		}
		err = s.setMotion(t, watch)
	}
	if err == nil {
		s.picker = nil
	}
	return err
}

func (s *layoutScreen) setMotion(t *config.Tile, watch string) error {
	blank, dwell := t.BlankAfterSeconds, t.DwellSeconds
	if t.Motion == "" {
		blank = 90
		dwell = 0
	}
	return s.edit("motion", t.ID, watch, strconv.Itoa(blank), strconv.Itoa(dwell))
}
