package uiassets

import (
	"io/fs"
	"testing"
)

func TestEmbeddedFiles(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"index.html", "app.css", "responsive.css", "app.js", "console-lib.js"} {
		bytes, err := fs.ReadFile(Files, name)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", name, err)
		}

		if len(bytes) == 0 {
			t.Fatalf("expected embedded file %q", name)
		}
	}
}
