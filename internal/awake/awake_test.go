package awake

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", s)
	return t
}

func TestEmbeddedCalendarParses(t *testing.T) {
	if len(Windows) == 0 {
		t.Fatal("no windows embedded")
	}
}

func TestActiveNextAndMergedEnd(t *testing.T) {
	windows, err := Parse("# yorum\n2026-11-07 2026-12-15 Ara\n2026-12-28 2027-02-08 Final\n2027-01-18 2027-02-07 Kayıt\n")
	if err != nil || len(windows) != 3 {
		t.Fatalf("parse: %v %d", err, len(windows))
	}
	if w, ok := Active(windows, at("2026-11-14 23:59")); !ok || w.Label != "Ara" {
		t.Fatalf("14 Kasım ara sınav dönemi olmalı: %+v %v", w, ok)
	}
	if _, ok := Active(windows, at("2026-12-15 23:00")); !ok {
		t.Fatal("bitiş günü dahil olmalı")
	}
	if _, ok := Active(windows, at("2026-12-16 00:01")); ok {
		t.Fatal("16 Aralık dönem dışı olmalı")
	}
	if next, ok := Next(windows, at("2026-12-20 12:00")); !ok || next.Label != "Final" {
		t.Fatalf("sıradaki dönem Final olmalı: %+v", next)
	}
	if end, ok := ActiveUntil(windows, at("2027-01-20 10:00")); !ok || end.Format("2006-01-02") != "2027-02-08" {
		t.Fatalf("üst üste binen dönemlerin sonu 8 Şubat olmalı: %v", end)
	}
	if _, ok := Next(windows, at("2027-02-09 10:00")); ok {
		t.Fatal("son dönemden sonra sıradaki dönem yok")
	}
}

func TestParseRejectsBadLines(t *testing.T) {
	for _, bad := range []string{"2026-11-07\n", "2026-13-01 2026-12-01 x\n", "2026-12-15 2026-11-07 ters\n"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
