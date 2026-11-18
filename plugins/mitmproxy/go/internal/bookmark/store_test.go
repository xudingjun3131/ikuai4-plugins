package bookmark_test

import (
	"testing"

	"github.com/GitterBois/proxying/internal/bookmark"
)

func Test_Nested_bookmarks(t *testing.T) {
	s, err := bookmark.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder, err := s.Add(bookmark.Node{Name: "API", Folder: true})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := s.Add(bookmark.Node{
		Name:     "users",
		ParentID: folder.ID,
		Method:   "GET",
		URL:      "https://api.example.com/users",
	})
	if err != nil {
		t.Fatal(err)
	}
	if leaf.ParentID != folder.ID {
		t.Fatalf("parent=%q", leaf.ParentID)
	}
	if _, err := s.Add(bookmark.Node{Name: "x", ParentID: "missing"}); err == nil {
		t.Fatal("want parent error")
	}
	list := s.List()
	if len(list) != 2 {
		t.Fatalf("list=%d", len(list))
	}
	if err := s.Delete(folder.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List()); n != 0 {
		t.Fatalf("after delete folder+child: %d", n)
	}
}
