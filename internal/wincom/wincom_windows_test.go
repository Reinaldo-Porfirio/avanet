package wincom

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	if err := Check(0, "op"); err != nil {
		t.Errorf("S_OK: %v", err)
	}
	if err := Check(sFalse, "op"); err != nil {
		t.Errorf("S_FALSE is a success: %v", err)
	}
	err := Check(0x80004005, "CoCreateInstance") // E_FAIL
	if err == nil || !strings.Contains(err.Error(), "CoCreateInstance") || !strings.Contains(err.Error(), "0x80004005") {
		t.Errorf("E_FAIL: %v", err)
	}
}

func TestRunNests(t *testing.T) {
	calls := 0
	err := Run(func() error {
		calls++
		return Run(func() error { calls++; return nil })
	})
	if err != nil || calls != 2 {
		t.Errorf("calls = %d, err = %v", calls, err)
	}
}
