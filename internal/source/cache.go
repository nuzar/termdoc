package source

import (
	"bytes"
	"io"
	"os"

	"github.com/nuzar/termdoc/internal/store"
)

// alreadyInstalled reports whether id is installed and should not be replaced.
func alreadyInstalled(st *store.Store, id string, force bool) error {
	if force {
		return nil
	}
	if _, err := os.Stat(st.Dir(id)); err == nil {
		return &installedError{id: id}
	}
	return nil
}

type installedError struct{ id string }

func (e *installedError) Error() string {
	return "docset " + quote(e.id) + " is already installed (use --force to replace it)"
}

func quote(s string) string {
	return `"` + s + `"`
}

// validArchive checks that path starts with a gzip or zip signature.
func validArchive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	n, _ := io.ReadFull(f, magic[:])
	if n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return true
	}
	return n >= 4 && magic[0] == 'P' && magic[1] == 'K'
}

// validJSONObject checks that path looks like a JSON object (DevDocs db.json).
func validJSONObject(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [512]byte
	n, _ := f.Read(buf[:])
	return bytes.HasPrefix(bytes.TrimSpace(buf[:n]), []byte("{"))
}

// replaceCache atomically moves src to dst, replacing an existing file.
// os.Rename does not overwrite on Windows, so remove dst first.
func replaceCache(src, dst string) error {
	_ = os.Remove(dst)
	return os.Rename(src, dst)
}
