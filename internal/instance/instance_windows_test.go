package instance

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestAcquireIsExclusiveUntilReleased(t *testing.T) {
	name := fmt.Sprintf("AvaNet-test-%d-%d", os.Getpid(), time.Now().UnixNano())

	first, err := Acquire(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(name); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Acquire: error = %v, want ErrAlreadyRunning", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}

	again, err := Acquire(name)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatal(err)
	}
}
