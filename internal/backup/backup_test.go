package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeSource struct {
	sig  string
	docs int
}

func (f *fakeSource) BackupTo(dst string) error {
	return os.WriteFile(dst, make([]byte, 100+f.docs), 0o644)
}
func (f *fakeSource) Counts() (int, int, int, error) { return f.docs, 0, 0, nil }
func (f *fakeSource) Signature() (string, error)     { return f.sig, nil }

func newManager(t *testing.T, keep int, ext string) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), func() int { return keep }, func() string { return ext }, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRunSkipsUnchangedAndNamesAreUnique(t *testing.T) {
	m := newManager(t, 30, "")
	src := &fakeSource{sig: "a", docs: 100}
	n1, err := m.Run(src, "auto", false)
	if err != nil || n1 == "" {
		t.Fatalf("first run: %q %v", n1, err)
	}
	if n, _ := m.Run(src, "auto", false); n != "" {
		t.Fatalf("unchanged content should not be backed up, got %s", n)
	}
	// Forced backups in the same second must not collide.
	a, err1 := m.Run(src, "manuelle", true)
	b, err2 := m.Run(src, "manuelle", true)
	if err1 != nil || err2 != nil || a == b {
		t.Fatalf("forced backups: %q %q %v %v", a, b, err1, err2)
	}
}

func TestShrinkAlertAndLargestKept(t *testing.T) {
	m := newManager(t, 5, "")
	src := &fakeSource{sig: "1", docs: 1000}
	m.Run(src, "auto", false)
	if m.Check(src) != "" {
		t.Fatal("no alert expected")
	}
	src.docs, src.sig = 3, "2"
	if m.Check(src) == "" {
		t.Fatal("alert expected after the content collapsed")
	}
	// Many small backups: pruning must keep the largest (the good one).
	for i := 0; i < 12; i++ {
		src.sig = string(rune('a' + i))
		m.Run(src, "auto", false)
	}
	var largest int64
	for _, b := range m.List() {
		if b.Size > largest {
			largest = b.Size
		}
	}
	if largest != 1100 {
		t.Fatalf("largest backup was pruned (largest now %d)", largest)
	}
	if st := m.Status(); st.MaxDocs != 1000 {
		t.Fatalf("reference must not drop to the collapsed size: %d", st.MaxDocs)
	}
}

func TestBackupFolderRecreated(t *testing.T) {
	m := newManager(t, 30, "")
	os.RemoveAll(m.Dir)
	if _, err := m.Run(&fakeSource{sig: "z", docs: 1}, "auto", false); err != nil {
		t.Fatalf("backup after the folder was deleted: %v", err)
	}
}

func TestExternalCopy(t *testing.T) {
	ext := t.TempDir()
	m := newManager(t, 30, ext)
	if _, err := m.Run(&fakeSource{sig: "x", docs: 5}, "auto", false); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(ext, "ArtiMan-sauvegardes"))
	if len(files) != 1 {
		t.Fatalf("external copies: %d", len(files))
	}
	st := m.Status()
	if !st.ExternalOK || st.ExternalLate || time.Since(st.LastExternal) > time.Minute {
		t.Fatalf("status = %+v", st)
	}
	if _, err := m.Path("../x.db"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
