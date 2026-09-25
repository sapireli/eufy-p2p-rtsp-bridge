package main

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"eufy-wall/internal/config"
)

func (s *layoutScreen) render(out io.Writer) error {
	if s.width < 80 || s.height < 24 {
		message := fmt.Sprintf("Resize terminal to at least 80x24 (now %dx%d); q quits.", s.width, s.height)
		if s.prompt == "quit" {
			message = "Unsaved edits: type DISCARD then Enter to quit, or Ctrl-G to cancel."
		} else if s.prompt != "" {
			message = s.promptLabel() + ": " + s.input + " [Enter accept, Ctrl-G cancel]"
		}
		_, err := io.WriteString(out, "\x1b[H\x1b[2J"+trimTerminal(message, max(0, s.width-1)))
		return err
	}
	c, err := s.editor.config()
	if err != nil {
		return err
	}
	if len(c.Tiles) > 0 && s.selected >= len(c.Tiles) {
		s.selected = len(c.Tiles) - 1
	}
	contentH := s.height - 6
	canvasW := c.Canvas.Cols
	if canvasW < 1 {
		canvasW = 32
	}
	if canvasW > 32 {
		canvasW = 32
	}
	panelW := s.width - canvasW - 5
	panel := s.panelLines(c, contentH, panelW)
	mode := "move"
	if s.resize {
		mode = "resize"
	}
	lines := make([]string, 0, s.height)
	lines = append(lines, trimTerminal(fmt.Sprintf("EUFY WALL  |  %dx%d canvas  |  %d tiles  |  %s by %d", c.Canvas.Cols, c.Canvas.Rows, len(c.Tiles), mode, s.step), s.width-1))
	lines = append(lines, strings.Repeat("-", min(s.width-1, 79)))
	for row := 0; row < contentH; row++ {
		canvas := s.canvasLine(c, row, contentH, canvasW)
		lines = append(lines, trimTerminal("|"+canvas+"| "+padTerminal(panel[row], panelW), s.width-1))
	}
	lines = append(lines, strings.Repeat("-", min(s.width-1, 79)))
	status := s.status
	if status == "" {
		if issues := s.editor.inventoryIssues(c); len(issues) > 0 {
			status = "inventory warning: " + issues[0]
		}
	}
	if status == "" {
		status = "Validated edits stay in memory until save or apply."
	}
	lines = append(lines, trimTerminal("Status: "+status, s.width-1))
	if s.prompt != "" {
		lines = append(lines, trimTerminal(s.promptLabel()+": "+s.input+"_  [Enter accept, Ctrl-U clear, Ctrl-G cancel]", s.width-1))
	} else if s.picker != nil {
		lines = append(lines, trimTerminal("Choose: arrows navigate, Enter accept, Space toggle, a all, Ctrl-G cancel", s.width-1))
	} else {
		lines = append(lines, "Tab select | arrows move | r resize | 1/4/8 step | e exact | c camera | w watch")
	}
	lines = append(lines, "u undo | y redo | i inventory | s save | A apply | ? help | q quit")
	_, err = io.WriteString(out, "\x1b[H\x1b[2J"+strings.Join(lines, "\r\n"))
	return err
}

func (s *layoutScreen) canvasLine(c *config.Config, row, height, width int) string {
	var b strings.Builder
	cols, rows := c.GridDims()
	for col := 0; col < width; col++ {
		x := (2*col + 1) * cols / (2 * width)
		y := (2*row + 1) * rows / (2 * height)
		symbol := '.'
		for i, tile := range c.Tiles {
			if tile.Rect != nil && x >= tile.Rect.X && x < tile.Rect.X+tile.Rect.W && y >= tile.Rect.Y && y < tile.Rect.Y+tile.Rect.H {
				symbol = tileSymbol(i)
				if i == s.selected {
					symbol = '@'
				}
				break
			}
		}
		b.WriteRune(symbol)
	}
	return b.String()
}

func (s *layoutScreen) panelLines(c *config.Config, height, width int) []string {
	panel := make([]string, height)
	if s.help {
		copy(panel, []string{
			"KEYBOARD HELP", "Tab/Shift-Tab: select tile", "Arrows: move or resize selection", "r: toggle move/resize; 1,4,8: step",
			"e: exact x y w h", "c: fixed camera; w: motion watch", "i: load live or exported inventory", "t: template; a: add; d: delete",
			"u/y: undo/redo; P: save PNG", "s: save draft; A: apply", "q: quit (asks if draft is unsaved)", "Ctrl-G: cancel prompt or picker", "?: close help",
		})
		return panel
	}
	if s.picker != nil {
		return s.pickerLines(height)
	}
	if len(c.Tiles) == 0 {
		panel[0] = "No tiles. Press a to add one."
		return panel
	}
	t := c.Tiles[s.selected]
	panel[0] = fmt.Sprintf("SELECTED %c  %s", tileSymbol(s.selected), t.ID)
	if t.Camera != "" {
		panel[1] = "Camera: " + t.Camera
	} else if t.Motion != "" {
		panel[1] = "Motion: latest"
	} else {
		panel[1] = "Source: " + t.URL
	}
	if t.Rect != nil {
		panel[2] = fmt.Sprintf("Grid: x=%d y=%d w=%d h=%d", t.Rect.X, t.Rect.Y, t.Rect.W, t.Rect.H)
	}
	if t.Motion != "" {
		watch := strings.Join(t.Watch, ",")
		if watch == "" {
			watch = "all"
		}
		panel[3] = "Watch: " + watch
		panel[4] = fmt.Sprintf("Blank: %ds  Dwell: %ds", t.BlankAfterSeconds, t.DwellSeconds)
	} else if s.editor.inventory != nil {
		if camera, ok := s.editor.inventory[t.Camera]; ok {
			panel[3] = "Name: " + camera.Name
			panel[4] = "Codec: " + camera.Codec + "  Mode: " + camera.Mode
		}
	}
	panel[5] = fmt.Sprintf("TILES (%d)", len(c.Tiles))
	visible := height - 6
	if visible < 1 {
		return panel
	}
	start := s.selected - visible/2
	if start < 0 {
		start = 0
	}
	if start > len(c.Tiles)-visible {
		start = len(c.Tiles) - visible
	}
	if start < 0 {
		start = 0
	}
	for row, index := 6, start; row < height && index < len(c.Tiles); row, index = row+1, index+1 {
		marker := " "
		if index == s.selected {
			marker = ">"
		}
		panel[row] = fmt.Sprintf("%s%c %s", marker, tileSymbol(index), c.Tiles[index].ID)
	}
	return panel
}

func (s *layoutScreen) pickerLines(height int) []string {
	p := s.picker
	panel := make([]string, height)
	panel[0] = "CHOOSE " + strings.ToUpper(p.kind)
	if p.kind == "watch" {
		panel[1] = "Space toggles; a watches all"
	}
	startRow := 2
	visible := height - startRow
	start := p.index - visible/2
	if start < 0 {
		start = 0
	}
	if start > len(p.serials)-visible {
		start = len(p.serials) - visible
	}
	if start < 0 {
		start = 0
	}
	for row, i := startRow, start; row < height && i < len(p.serials); row, i = row+1, i+1 {
		cursor, check := " ", " "
		if i == p.index {
			cursor = ">"
		}
		if p.kind == "watch" && p.checked[p.serials[i]] && !p.all {
			check = "x"
		}
		if p.kind == "watch" {
			panel[row] = fmt.Sprintf("%s[%s] %s", cursor, check, p.serials[i])
		} else {
			panel[row] = cursor + " " + p.serials[i]
		}
	}
	if p.all && p.kind == "watch" {
		panel[1] = "Watching all; Space picks a subset"
	}
	return panel
}

func (s *layoutScreen) promptLabel() string {
	switch s.prompt {
	case "rect":
		return "Exact rectangle x y w h"
	case "camera":
		return "Camera serial"
	case "watch":
		return "Watch serials comma-separated or all"
	case "delete":
		return "Type DELETE to remove tile"
	case "save":
		return "Draft path"
	case "apply":
		return "Type APPLY to activate"
	case "inventory":
		return "Inventory file (blank fetches bridge)"
	case "template":
		return "Template one|split|four|one-plus-five|motion"
	case "add":
		return "Add id camera x y w h"
	case "png":
		return "PNG path"
	case "quit":
		return "Type DISCARD to quit without saving"
	}
	return s.prompt
}

func trimTerminal(text string, width int) string {
	if width < 0 {
		width = 0
	}
	var b strings.Builder
	for _, r := range text {
		if b.Len() >= width {
			break
		}
		if !unicode.IsPrint(r) || r > 126 {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func padTerminal(text string, width int) string {
	text = trimTerminal(text, width)
	return text + strings.Repeat(" ", max(0, width-len(text)))
}
