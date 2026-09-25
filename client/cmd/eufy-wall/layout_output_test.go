package main

import "testing"

func TestLayoutScreenCanSelectNamedInstanceOutput(t *testing.T) {
	target, _ := targetForPlatform("linux", "", "right")
	editor, err := newLayoutEditorForTarget("", target)
	if err != nil {
		t.Fatal(err)
	}
	screen := &layoutScreen{editor: editor, step: 1}
	if _, err := screen.handle(screenKey{r: 'o'}); err != nil || screen.prompt != "output" {
		t.Fatalf("output prompt did not open: prompt=%q err=%v", screen.prompt, err)
	}
	if err := screen.executePrompt("output", "HDMI-A-2"); err != nil {
		t.Fatal(err)
	}
	c, err := editor.config()
	if err != nil || c.Output != "HDMI-A-2" || validateTargetOutput(target, c) != nil {
		t.Fatalf("output was not saved in editor history: %+v, %v", c, err)
	}
	if err := screen.executePrompt("output", "../bad"); err == nil {
		t.Fatal("invalid output was accepted")
	}
	c, _ = editor.config()
	if c.Output != "HDMI-A-2" {
		t.Fatal("rejected output changed the draft")
	}
}
