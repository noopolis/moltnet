package daimon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A failed chmod or write of the temporary file must fail the save, never be
// discarded. Both errors used to land in a variable shadowed by the enclosing
// `if err := ...` statement, so `save` read the still-nil outer `err`, reported
// success, and renamed the temporary file over the real store anyway. For a
// failed chmod that published an EMPTY receipt store; for a failed write it
// reported a durable receipt that was never written.
func TestSaveReportsChmodAndWriteFailures(t *testing.T) {
	failure := errors.New("injected receipt store failure")

	for _, testCase := range []struct {
		name    string
		install func()
	}{
		{
			name: "chmod fails",
			install: func() {
				receiptStoreChmod = func(*os.File, os.FileMode) error { return failure }
			},
		},
		{
			name: "write fails",
			install: func() {
				receiptStoreWrite = func(*os.File, []byte) (int, error) { return 0, failure }
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			storePath := filepath.Join(t.TempDir(), "private", "receipts.json")
			store, err := openReceiptStore(storePath)
			if err != nil {
				t.Fatal(err)
			}

			chmod, write := receiptStoreChmod, receiptStoreWrite
			t.Cleanup(func() { receiptStoreChmod, receiptStoreWrite = chmod, write })
			testCase.install()

			putErr := store.Put(trackerTestJob())

			if putErr == nil {
				t.Fatal("save reported success after the temporary file could not be prepared")
			}
			if !errors.Is(putErr, failure) {
				t.Fatalf("save did not surface the underlying failure: %v", putErr)
			}
			// The atomic publish must not have happened: a store that could not
			// be written must leave no file behind rather than an empty or
			// partial one that a restart would read as an empty receipt store.
			if _, statErr := os.Stat(storePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("a failed save published the store anyway: %v", statErr)
			}
			// And it must not leak the temporary file it created.
			entries, readErr := os.ReadDir(filepath.Dir(storePath))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, entry := range entries {
				t.Fatalf("failed save left %q behind", entry.Name())
			}
		})
	}
}

// The success path still writes the whole document and publishes it privately,
// so the guard above cannot be satisfied by refusing to save at all.
func TestSaveWritesCompleteContentsOnSuccess(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "private", "receipts.json")
	store, err := openReceiptStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(trackerTestJob()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt store mode = %v", info.Mode().Perm())
	}
	if info.Size() == 0 {
		t.Fatal("receipt store was published empty")
	}
	reopened, err := openReceiptStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if pending := reopened.Pending(); len(pending) != 1 {
		t.Fatalf("pending jobs after reopen = %#v", pending)
	}
}
