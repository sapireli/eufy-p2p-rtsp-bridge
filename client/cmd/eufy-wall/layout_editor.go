package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"eufy-wall/internal/config"
	"gopkg.in/yaml.v3"
)

// layoutEditor keeps validated YAML snapshots. A rejected command never enters history, so undo and
// redo cannot resurrect an invalid configuration. No active file changes until the explicit apply.
type layoutEditor struct {
	history   [][]byte
	at        int
	source    string
	inventory map[string]setupCamera
}

var errLegacyLayout = errors.New("legacy layout needs migration")

func newLayoutEditor(path string) (*layoutEditor, error) {
	var data []byte
	var err error
	if path == "" {
		data = config.Example()
	} else if path == "-" {
		return nil, errors.New("layout edit needs a file path; stdin is reserved for editor commands")
	} else {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}
	c, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	if c.SchemaVersion != 2 {
		return nil, errLegacyLayout
	}
	if c.Layout != "custom" {
		return nil, errors.New("layout edit requires layout: custom; use an explicit rectangle template")
	}
	if _, err := placeForValidation(c); err != nil {
		return nil, err
	}
	return &layoutEditor{history: [][]byte{data}, source: path}, nil
}

func (e *layoutEditor) config() (*config.Config, error) { return config.Parse(e.history[e.at]) }

func (e *layoutEditor) change(mutate func(*config.Config) error) error {
	c, err := e.config()
	if err != nil {
		return err
	}
	if err := mutate(c); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	parsed, err := config.Parse(b)
	if err != nil {
		return err
	}
	if _, err := placeForValidation(parsed); err != nil {
		return err
	}
	e.history = append(e.history[:e.at+1], b)
	e.at++
	return nil
}

func (e *layoutEditor) undo() bool {
	if e.at == 0 {
		return false
	}
	e.at--
	return true
}

func (e *layoutEditor) redo() bool {
	if e.at+1 == len(e.history) {
		return false
	}
	e.at++
	return true
}

// editLayout is a keyboard-only editor. It works through SSH, accepts scripted input, and never
// overwrites the active config through its draft-saving path.
func editLayout(path string, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	e, err := newLayoutEditor(path)
	if errors.Is(err, errLegacyLayout) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		converted, diff, migrateErr := migrateLegacyForEditor(data)
		if migrateErr != nil {
			return migrateErr
		}
		if _, writeErr := fmt.Fprintf(out, "Legacy migration preview (active config is untouched):\n%s\nType MIGRATE to edit this v2 draft: ", diff); writeErr != nil {
			return writeErr
		}
		if !scanner.Scan() {
			if scanner.Err() != nil {
				return scanner.Err()
			}
			return errors.New("legacy migration was not accepted")
		}
		if strings.TrimSpace(scanner.Text()) != "MIGRATE" {
			return errors.New("legacy migration was not accepted")
		}
		e, err = newLayoutEditorFromData(converted, path)
	}
	if err != nil {
		return err
	}
	if input, output, ok := fullScreenTerminal(in, out); ok {
		return runLayoutScreen(e, input, output)
	}
	if _, err := io.WriteString(out, editorHelp); err != nil {
		return err
	}
	if err := e.show(out); err != nil {
		return err
	}
	for {
		if _, err := io.WriteString(out, "layout> "); err != nil {
			return err
		}
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "quit", "exit":
			return nil
		case "help":
			_, err = io.WriteString(out, editorHelp)
		case "show", "preview":
			err = e.show(out)
		case "undo":
			if !e.undo() {
				err = errors.New("nothing to undo")
			} else {
				err = e.show(out)
			}
		case "redo":
			if !e.redo() {
				err = errors.New("nothing to redo")
			} else {
				err = e.show(out)
			}
		case "save":
			target := strings.TrimSpace(strings.TrimPrefix(line, "save"))
			if target == "" {
				target = "eufy-wall.draft.yaml"
			}
			err = e.save(target)
			if err == nil {
				_, err = fmt.Fprintf(out, "draft saved: %s\n", target)
			}
		case "apply":
			if err = e.ensureInventory(""); err == nil {
				err = e.checkInventory()
			}
			if err == nil {
				err = applyClientConfig("-", bytes.NewReader(e.history[e.at]), out)
			}
		case "inventory":
			if len(fields) > 2 {
				err = errors.New("usage: inventory [exported-json-file]")
			} else {
				file := ""
				if len(fields) == 2 {
					file = fields[1]
				}
				if err = e.ensureInventory(file); err == nil {
					err = e.showInventory(out)
				}
				if err == nil {
					err = e.show(out)
				}
			}
		case "png":
			if len(fields) != 2 {
				err = errors.New("usage: png <path>")
			} else {
				err = e.png(fields[1])
			}
		case "template", "add", "delete", "rect", "move", "resize", "camera", "motion":
			err = e.change(func(c *config.Config) error { return editorMutation(c, fields) })
			if err == nil {
				err = e.show(out)
			}
		default:
			err = fmt.Errorf("unknown command %q; type help", fields[0])
		}
		if err != nil {
			if _, writeErr := fmt.Fprintf(out, "error: %v\n", err); writeErr != nil {
				return writeErr
			}
		}
	}
}

func newLayoutEditorFromData(data []byte, source string) (*layoutEditor, error) {
	c, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	if c.SchemaVersion != 2 || c.Layout != "custom" {
		return nil, errors.New("editor data must be a v2 custom layout")
	}
	if _, err := placeForValidation(c); err != nil {
		return nil, err
	}
	return &layoutEditor{history: [][]byte{data}, source: source}, nil
}

func (e *layoutEditor) show(out io.Writer) error {
	c, err := e.config()
	if err != nil {
		return err
	}
	p, err := placeForValidation(c)
	if err != nil {
		return err
	}
	screen := c.Screen
	if screen.Width == 0 || screen.Height == 0 {
		screen = config.Screen{Width: 1920, Height: 1080}
	}
	if err := textLayout(out, c, p, screen); err != nil {
		return err
	}
	for _, issue := range e.inventoryIssues(c) {
		if _, err := fmt.Fprintf(out, "inventory warning: %s\n", issue); err != nil {
			return err
		}
	}
	return nil
}

func (e *layoutEditor) save(target string) error {
	active, _ := filepath.Abs(clientConfigPath)
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if abs == active {
		return errors.New("save drafts outside the active config; use apply to activate")
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved == active {
		return errors.New("draft target points to the active config; use apply to activate")
	}
	return atomicClientWrite(abs, e.history[e.at], 0600, nil)
}

func (e *layoutEditor) png(path string) error {
	c, err := e.config()
	if err != nil {
		return err
	}
	p, err := placeForValidation(c)
	if err != nil {
		return err
	}
	return writeLayoutPNG(path, c, p)
}

const editorHelp = `Commands (all keyboard accessible; exact grid coordinates are shown after each edit):
  show                           Show scaled canvas and tile coordinates
  template one|split|four|one-plus-five|motion
  add <id> <camera> <x> <y> <w> <h>
  rect <id> <x> <y> <w> <h>    Set exact rectangle
  move <id> <dx> <dy>         Move by signed grid cells
  resize <id> <dw> <dh>       Resize by signed grid cells
  camera <id> <serial>        Make a fixed tile
  motion <id> <serial,...|all> [blank_seconds] [dwell_seconds]
  delete <id>                 Remove tile
  undo | redo                 Revisit validated edits
  png <path>                  Save numbered preview PNG
  inventory [exported.json]   Load bridge or offline camera inventory
  save [path]                 Save a draft (default: eufy-wall.draft.yaml)
  apply                       Validate and safely apply to /etc/eufy-wall.yaml
  help | quit                 Show help or exit without changing active config

`
