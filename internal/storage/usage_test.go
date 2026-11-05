package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

func TestDataUsageAccumulatesPerDayAndClass(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	const today, yesterday = domain.Day("2026-11-05"), domain.Day("2026-11-04")

	for _, add := range []struct {
		day   domain.Day
		class domain.DataClass
		bytes int64
	}{
		{today, domain.DataFixed, 90 * domain.MB},
		{today, domain.DataFixed, 10 * domain.MB},
		{today, domain.DataMobile, 5 * domain.MB},
		{yesterday, domain.DataFixed, 1 * domain.MB},
	} {
		if err := db.AddDataUsage(ctx, add.day, add.class, add.bytes); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		day   domain.Day
		class domain.DataClass
		want  int64
	}{
		{today, domain.DataFixed, 100 * domain.MB},
		{today, domain.DataMobile, 5 * domain.MB},
		{yesterday, domain.DataFixed, 1 * domain.MB},
		{yesterday, domain.DataMobile, 0}, // never written
	}
	for _, tt := range tests {
		got, err := db.DataUsage(ctx, tt.day, tt.class)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%s/%v = %d, want %d", tt.day, tt.class, got, tt.want)
		}
	}
}

func TestDataUsageRejectsInvalidInput(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	cases := map[string]error{
		"bad day":        db.AddDataUsage(ctx, "05/11/2026", domain.DataFixed, 1),
		"negative bytes": db.AddDataUsage(ctx, "2026-11-05", domain.DataFixed, -1),
		"unknown class":  db.AddDataUsage(ctx, "2026-11-05", domain.DataClass(9), 1),
	}
	for name, err := range cases {
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", name, err)
		}
	}
	if _, err := db.DataUsage(ctx, "", domain.DataFixed); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("read with empty day: error = %v, want ErrInvalidInput", err)
	}
}
