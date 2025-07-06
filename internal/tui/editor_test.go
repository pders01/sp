package tui

import (
	"testing"
)

func TestEditorInitialState(t *testing.T) {
	date := "2024-01-01"
	content := "Initial content"
	ed := NewEditor(date, content)

	if ed.date != date {
		t.Errorf("expected date %q, got %q", date, ed.date)
	}
	if ed.GetContent() != content {
		t.Errorf("expected content %q, got %q", content, ed.GetContent())
	}
	if ed.IsQuitting() {
		t.Error("expected IsQuitting to be false initially")
	}
}

func TestEditorSetContent(t *testing.T) {
	ed := NewEditor("2024-01-01", "")
	newContent := "Some new content"
	ed.textarea.SetValue(newContent)
	if ed.GetContent() != newContent {
		t.Errorf("expected content %q, got %q", newContent, ed.GetContent())
	}
}
