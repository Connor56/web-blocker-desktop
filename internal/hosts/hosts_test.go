package hosts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTransformAddsManagedSection(t *testing.T) {
	original := []byte("127.0.0.1 localhost\n# user content\n")
	got, changed, err := Transform(original, true, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a change")
	}
	want := appendCopy(original, []byte("\n"+beginMarker+"\n127.0.0.1 example.com\n::1 example.com\n"+endMarker+"\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTransformIsIdempotent(t *testing.T) {
	first, _, err := Transform([]byte("original\n"), true, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	second, changed, err := Transform(first, true, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if changed || !bytes.Equal(first, second) {
		t.Fatal("second transform should make no change")
	}
}

func TestTransformReplacesOnlyManagedContent(t *testing.T) {
	original := []byte("before\n\n" + beginMarker + "\n127.0.0.1 old.example\n::1 old.example\n" + endMarker + "\nafter\n")
	got, _, err := Transform(original, true, []string{"new.example"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("before\n\n" + beginMarker + "\n127.0.0.1 new.example\n::1 new.example\n" + endMarker + "\nafter\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTransformRemovesSectionAndRestoresOutsideBytes(t *testing.T) {
	original := []byte("one\r\ntwo")
	active, _, err := Transform(original, true, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	got, changed, err := Transform(active, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !bytes.Equal(got, original) {
		t.Fatalf("got %q, want original %q", got, original)
	}
}

func TestTransformRejectsMalformedMarkers(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(beginMarker + "\n"),
		[]byte(endMarker + "\n" + beginMarker + "\n"),
		[]byte(beginMarker + "\n" + endMarker + "\n" + endMarker + "\n"),
	} {
		if _, _, err := Transform(data, true, []string{"example.com"}); err == nil {
			t.Fatalf("expected malformed markers in %q to fail", data)
		}
	}
}

func TestManagerAvoidsUnnecessaryRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path)
	changed, err := manager.Reconcile(true, []string{"example.com"})
	if err != nil || !changed {
		t.Fatalf("first reconcile: changed=%v err=%v", changed, err)
	}
	info1, _ := os.Stat(path)
	changed, err = manager.Reconcile(true, []string{"example.com"})
	if err != nil || changed {
		t.Fatalf("second reconcile: changed=%v err=%v", changed, err)
	}
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("unchanged hosts file was rewritten")
	}
}

func TestManagerDetectsEditsWithUnchangedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path)
	if _, err := manager.Reconcile(true, []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("example.com"), []byte("changed.com"))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	changed, err := manager.Reconcile(true, []string{"example.com"})
	if err != nil || !changed {
		t.Fatalf("changed=%v error=%v", changed, err)
	}
}
