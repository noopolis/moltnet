package webhook

import (
	"path/filepath"
	"testing"
)

func TestReceiptStoreRejectsConcurrentRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.json")
	unlock, err := lockReceipts(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockReceipts(path); err == nil {
		second()
		unlock()
		t.Fatal("parallel runner bypassed receipt budget")
	}
	unlock()
	next, err := lockReceipts(path)
	if err != nil {
		t.Fatal("lock not released:", err)
	}
	next()
}
