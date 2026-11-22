package usbcopy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDestinationIndexDoesNotTraverseExistingImportFolders(t *testing.T) {
	root := t.TempDir()
	name := "USB Copy - MP3 2 - 2026-11-21"
	if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent", filepath.Join(root, name, "unrelated-link")); err != nil {
		t.Fatal(err)
	}
	index, err := indexDestination(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 {
		t.Fatalf("descendants indexed: %v", index)
	}
	allocated, err := allocateDestination(name, index)
	if err != nil || allocated == name {
		t.Fatal("existing directory reused")
	}
	index[pathKey(allocated)] = true
	if _, err = allocateDestination(allocated+"/song.mp3", index); err != nil {
		t.Fatal(err)
	}
}
func TestDestinationIndexRejectsTopLevelSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("/nonexistent", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := indexDestination(root); err == nil {
		t.Fatal("top-level symlink accepted")
	}
}
