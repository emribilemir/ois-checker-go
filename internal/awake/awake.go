// Package awake botun hangi tarihlerde uyanık kalacağını söyler.
//
// Render'ın ücretsiz servisi istek gelmezse uyur. Bot yalnız sınav ve not
// dönemlerinde ve ders seçimi sırasında kendini uyanık tutar; diğer zamanlarda
// uyur ve çalışma saati kotasını harcamaz. Dönemler windows.txt'tedir.
package awake

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed windows.txt
var windowsFile string

// Window uyanık kalınacak bir dönemdir; Start ve End günleri dahildir.
type Window struct {
	Start, End time.Time
	Label      string
}

// Windows gömülü dönem listesidir.
var Windows = mustParse(windowsFile)

func mustParse(text string) []Window {
	windows, err := Parse(text)
	if err != nil {
		panic(err)
	}
	return windows
}

// Parse "YYYY-MM-DD YYYY-MM-DD açıklama" satırlarını okur; # ile başlayanlar yorumdur.
func Parse(text string) ([]Window, error) {
	var windows []Window
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) < 2 {
			return nil, fmt.Errorf("windows.txt satır %d: iki tarih gerekli", n+1)
		}
		start, err1 := time.Parse("2006-01-02", fields[0])
		end, err2 := time.Parse("2006-01-02", fields[1])
		if err1 != nil || err2 != nil || end.Before(start) {
			return nil, fmt.Errorf("windows.txt satır %d: geçersiz tarih", n+1)
		}
		label := ""
		if len(fields) == 3 {
			label = strings.TrimSpace(fields[2])
		}
		windows = append(windows, Window{Start: start, End: end, Label: label})
	}
	return windows, nil
}

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

// Active, now gününü kapsayan dönemlerden en geç biteni döndürür.
func Active(windows []Window, now time.Time) (Window, bool) {
	today := day(now)
	var best Window
	found := false
	for _, w := range windows {
		if !today.Before(w.Start) && !today.After(w.End) && (!found || w.End.After(best.End)) {
			best, found = w, true
		}
	}
	return best, found
}

// ActiveUntil, birbirine değen ya da üst üste binen dönemleri birleştirerek
// botun kesintisiz uyanık kalacağı son günü döndürür.
func ActiveUntil(windows []Window, now time.Time) (time.Time, bool) {
	w, ok := Active(windows, now)
	if !ok {
		return time.Time{}, false
	}
	end := w.End
	for changed := true; changed; {
		changed = false
		for _, other := range windows {
			if !other.Start.After(end.AddDate(0, 0, 1)) && other.End.After(end) && !other.End.Before(day(now)) {
				end, changed = other.End, true
			}
		}
	}
	return end, true
}

// Next, now'dan sonra başlayan ilk dönemi döndürür.
func Next(windows []Window, now time.Time) (Window, bool) {
	today := day(now)
	var best Window
	found := false
	for _, w := range windows {
		if w.Start.After(today) && (!found || w.Start.Before(best.Start)) {
			best, found = w, true
		}
	}
	return best, found
}
