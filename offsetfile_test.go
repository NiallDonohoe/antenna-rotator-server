package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOffsetFileRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "offset")

	if _, ok, err := loadOffset(path); err != nil || ok {
		t.Fatalf("loadOffset before any save: got ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	for _, want := range []int{15, 0, 355} {
		if err := saveOffset(path, want); err != nil {
			t.Fatalf("saveOffset(%d): %v", want, err)
		}
		got, ok, err := loadOffset(path)
		if err != nil || !ok || got != want {
			t.Fatalf("loadOffset after saving %d: got %d ok=%v err=%v", want, got, ok, err)
		}
	}

	// Only the offset file itself should remain; no temp files left over.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the offset file in the directory, found %d entries", len(entries))
	}
}

func TestLoadOffsetRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "offset")
	if err := os.WriteFile(path, []byte("north\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadOffset(path); err == nil {
		t.Fatal("expected an error for a non-integer offset file")
	}
}

func TestSaveOffsetFailsForMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "offset")
	if err := saveOffset(path, 10); err == nil {
		t.Fatal("expected an error when the directory does not exist")
	}
}

func TestInitialOffsetPrefersSavedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "offset")
	t.Setenv("ROTATOR_HEADING_OFFSET", "20")

	got, err := initialOffset(path)
	if err != nil || got != 20 {
		t.Fatalf("no saved file: got %d err=%v, want the env value 20", got, err)
	}

	if err := saveOffset(path, 7); err != nil {
		t.Fatal(err)
	}
	got, err = initialOffset(path)
	if err != nil || got != 7 {
		t.Fatalf("saved file present: got %d err=%v, want the saved value 7", got, err)
	}

	got, err = initialOffset("")
	if err != nil || got != 20 {
		t.Fatalf("no offset file configured: got %d err=%v, want the env value 20", got, err)
	}
}
