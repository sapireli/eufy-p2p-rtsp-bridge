package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (s *layoutScreen) handlePrompt(key screenKey) (bool, error) {
	if key.name == "cancel" || key.name == "escape" {
		s.prompt, s.input = "", ""
		s.status = "cancelled"
		return false, nil
	}
	if key.name == "backspace" {
		if len(s.input) > 0 {
			_, size := utf8.DecodeLastRuneInString(s.input)
			s.input = s.input[:len(s.input)-size]
		}
		return false, nil
	}
	if key.name == "clear" {
		s.input = ""
		return false, nil
	}
	if key.name == "enter" {
		kind, value := s.prompt, strings.TrimSpace(s.input)
		if kind == "quit" {
			s.prompt, s.input = "", ""
			if value == "DISCARD" {
				return true, nil
			}
			s.status = "quit cancelled"
			return false, nil
		}
		if err := s.executePrompt(kind, value); err != nil {
			return false, err
		}
		s.prompt, s.input = "", ""
		return false, nil
	}
	if key.r != 0 && unicode.IsPrint(key.r) && len(s.input) < 4096 {
		s.input += string(key.r)
	}
	return false, nil
}

func (s *layoutScreen) executePrompt(kind, value string) error {
	switch kind {
	case "rect":
		t, err := s.tile()
		if err != nil {
			return err
		}
		fields := strings.Fields(value)
		return s.edit(append([]string{"rect", t.ID}, fields...)...)
	case "camera":
		t, err := s.tile()
		if err != nil {
			return err
		}
		if value == "" || strings.ContainsAny(value, " \t\r\n") {
			return errors.New("enter one camera serial")
		}
		if s.editor.inventory != nil {
			if _, ok := s.editor.inventory[value]; !ok {
				return fmt.Errorf("camera %q is not in inventory", value)
			}
		}
		return s.edit("camera", t.ID, value)
	case "watch":
		t, err := s.tile()
		if err != nil {
			return err
		}
		if value == "" {
			return errors.New("enter comma-separated serials or all")
		}
		if s.editor.inventory != nil && value != "all" {
			for _, sn := range strings.Split(value, ",") {
				if _, ok := s.editor.inventory[sn]; !ok {
					return fmt.Errorf("camera %q is not in inventory", sn)
				}
			}
		}
		return s.setMotion(t, value)
	case "delete":
		if value != "DELETE" {
			return errors.New("type DELETE to remove this tile")
		}
		t, err := s.tile()
		if err != nil {
			return err
		}
		return s.edit("delete", t.ID)
	case "save":
		if value == "" {
			value = "eufy-wall.draft.yaml"
		}
		if err := s.editor.save(value); err != nil {
			return err
		}
		s.saved = bytes.Clone(s.editor.history[s.editor.at])
		s.status = "draft saved: " + value
		return nil
	case "apply":
		if value != "APPLY" {
			return errors.New("type APPLY to activate the validated config")
		}
		if s.apply == nil {
			return errors.New("apply is unavailable")
		}
		if err := s.apply(); err != nil {
			return err
		}
		s.saved = bytes.Clone(s.editor.history[s.editor.at])
		return nil
	case "inventory":
		if err := s.editor.ensureInventory(value); err != nil {
			return err
		}
		s.status = fmt.Sprintf("loaded %d cameras from inventory", len(s.editor.inventory))
		return nil
	case "template":
		if err := s.edit("template", value); err != nil {
			return err
		}
		s.selected = 0
		return nil
	case "add":
		fields := strings.Fields(value)
		if err := s.edit(append([]string{"add"}, fields...)...); err != nil {
			return err
		}
		c, err := s.editor.config()
		if err != nil {
			return err
		}
		s.selected = len(c.Tiles) - 1
		return nil
	case "png":
		if value == "" {
			return errors.New("enter a PNG path")
		}
		if err := s.editor.png(value); err != nil {
			return err
		}
		s.status = "PNG saved: " + value
		return nil
	}
	return fmt.Errorf("unknown prompt %q", kind)
}
