package fsx

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	defer syscall.Umask(syscall.Umask(0o022))
	dir := t.TempDir()
	p := filepath.Join(dir, "task.md")
	for _, data := range []string{"first\n", "second, longer\n", ""} {
		if err := WriteAtomic(p, []byte(data), 0o664); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(p); err != nil || string(got) != data {
			t.Fatalf("read back %q, %v; want %q", got, err, data)
		}
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, %v; want 0644 (0664 less the umask)", fi.Mode(), err)
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("left behind: %v", es)
	}
}

func TestWriteAtomicFails(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAtomic(filepath.Join(dir, "nosuch", "f"), []byte("x"), 0o644); err == nil {
		t.Fatal("no error writing into a missing dir")
	}
	// Renaming over a directory fails; the temporary file is removed.
	p := filepath.Join(dir, "d")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("x"), 0o644); err == nil {
		t.Fatal("no error replacing a directory")
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("left behind: %v", es)
	}
}
