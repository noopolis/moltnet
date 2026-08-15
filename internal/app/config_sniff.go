package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// configWarnWriter is where warnIgnoringBinaryConfig writes; a var, not a
// direct os.Stderr call, so tests can capture the warning instead of it
// landing on the real process stderr.
var configWarnWriter io.Writer = os.Stderr

// looksLikeTextConfig reports whether path's first 512 bytes read as plain
// text: valid UTF-8 with no embedded NUL byte. It exists so config
// discovery can tell a genuine YAML/JSON config file apart from a stray
// compiled binary that happens to share the config filename — most
// plausibly ./moltnet (a build output) matching the config file "Moltnet"
// on a case-insensitive filesystem such as APFS, which macOS will resolve
// to the binary regardless of case. Only the sample is read (io.ReadFull
// into a fixed 512-byte buffer), not the whole file: a short file trips
// io.ErrUnexpectedEOF/io.EOF, which is tolerated, not treated as a read
// failure. Any other read error is returned as-is; callers treat it the
// same as any other stat/read failure.
func looksLikeTextConfig(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	sample := make([]byte, 512)
	n, err := io.ReadFull(file, sample)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	return textSniffContents(sample[:n]), nil
}

// textSniffContents applies the same shallow text/binary signature to an
// in-memory buffer, used when the caller already has the file's bytes
// (loadFileConfig) so the file is not read twice.
func textSniffContents(contents []byte) bool {
	sample := contents
	if len(sample) > 512 {
		sample = sample[:512]
	}
	for _, b := range sample {
		if b == 0 {
			return false
		}
	}
	if utf8.Valid(sample) {
		return true
	}
	// sample may have been cut off mid-rune — either by the truncation
	// above, or because the caller (looksLikeTextConfig) already read only
	// a fixed-size prefix of the file. A multibyte UTF-8 rune whose bytes
	// straddle that cutoff would otherwise make an entirely valid text file
	// look binary. Trim up to utf8.UTFMax-1 trailing bytes until the sample
	// is valid UTF-8 again before giving up.
	for i := 0; i < utf8.UTFMax-1 && len(sample) > 0; i++ {
		sample = sample[:len(sample)-1]
		if utf8.Valid(sample) {
			return true
		}
	}
	return false
}

// warnIgnoringBinaryConfig prints the stderr line a discovered (not
// explicitly named) config candidate gets when it fails the text sniff:
// resolution treats it as though it were never there and keeps looking.
func warnIgnoringBinaryConfig(path string) {
	fmt.Fprintf(configWarnWriter, "warning: ignoring %s: not a text config file (did a compiled binary shadow the config filename on a case-insensitive filesystem?)\n", path)
}

// binaryConfigError is the clearer error an explicitly named config path
// (--config, MOLTNET_CONFIG) surfaces instead of a raw YAML/JSON decode
// failure when the file is not text.
func binaryConfigError(path string) error {
	return fmt.Errorf("moltnet config %q does not look like a text config file (did a compiled binary shadow the config filename on a case-insensitive filesystem?)", path)
}
