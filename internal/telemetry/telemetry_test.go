package telemetry

import (
	"strings"
	"testing"
)

func TestSnapshotKeepsCountersAndNewestEventsFirst(t *testing.T) {
	Reset()
	Inc("cycle")
	Inc("cycle")
	Set("last_cycle_duration", "2.1s")
	for i := 0; i < maxEvents+5; i++ {
		Note("login_fail", "ocr_fail")
	}
	Note("start", "sürüm abc")
	snap := Snapshot()
	if snap["counters"].(map[string]int64)["cycle"] != 2 || snap["values"].(map[string]string)["last_cycle_duration"] != "2.1s" {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
	recent := snap["recent"].([]Event)
	if len(recent) != maxEvents || recent[0].Kind != "start" {
		t.Fatalf("expected %d events, newest first; got %d, first=%+v", maxEvents, len(recent), recent[0])
	}
}

func TestTextIsScrubbedAndTruncated(t *testing.T) {
	Reset()
	Note("check_failed", "final=https://ois.example/ogrenciler/belge/ogrsinavsonuc/ogrenci_no/240123456 TC 11111111111")
	Note("long", strings.Repeat("ş", 400))
	last := Snapshot()["last"].(map[string]Event)
	if strings.Contains(last["check_failed"].Text, "240123456") || strings.Contains(last["check_failed"].Text, "11111111111") || !strings.Contains(last["check_failed"].Text, "ogrenci_no/<no>") {
		t.Fatalf("long numbers not masked: %q", last["check_failed"].Text)
	}
	if len(last["long"].Text) > maxText+len("…") || !strings.HasSuffix(last["long"].Text, "…") {
		t.Fatalf("text not truncated on a rune boundary: %d bytes", len(last["long"].Text))
	}
}
