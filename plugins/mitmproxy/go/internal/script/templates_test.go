package script_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/script"
)

func Test_Gallery_List_and_Apply(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	g := script.NewGallery(scripts)
	list := g.List()
	if len(list) < 2 {
		t.Fatalf("bundled templates: %d", len(list))
	}
	name, err := g.Apply("add-header", "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "add-header.js" {
		t.Fatalf("name=%q", name)
	}
	b, err := os.ReadFile(filepath.Join(scripts, name))
	if err != nil || len(b) == 0 {
		t.Fatalf("written: %v %q", err, b)
	}
	r, err := script.NewRunner(dir, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range r.ListInstalled() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("runner missing applied script: %v", r.ListInstalled())
	}
}

func Test_Gallery_Apply_unknown(t *testing.T) {
	g := script.NewGallery(t.TempDir())
	if _, err := g.Apply("nope", ""); err == nil {
		t.Fatal("want error")
	}
}
