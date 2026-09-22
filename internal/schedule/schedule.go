package schedule

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
	"unicode"

	htmlnode "golang.org/x/net/html"
)

type Entry struct {
	Day      time.Weekday
	Code     string
	Name     string
	Location string
	Start    string
	End      string
	Online   bool
}

type Reminder struct {
	Entry Entry
	Key   string
}

var dayNames = map[time.Weekday]string{
	time.Monday: "Pazartesi", time.Tuesday: "Salı", time.Wednesday: "Çarşamba",
	time.Thursday: "Perşembe", time.Friday: "Cuma", time.Saturday: "Cumartesi", time.Sunday: "Pazar",
}

func Parse(body []byte) ([]Entry, error) {
	doc, err := htmlnode.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("ders programı HTML: %w", err)
	}
	var tables []*htmlnode.Node
	findElements(doc, "table", &tables)
	for _, table := range tables {
		rows := tableRows(table)
		if len(rows) < 2 {
			continue
		}
		headers := directCells(rows[0])
		if len(headers) != 7 {
			continue
		}
		days := make([]time.Weekday, 0, 7)
		seen := map[time.Weekday]bool{}
		for _, header := range headers {
			day, ok := parseDay(nodeText(header))
			if !ok || seen[day] {
				break
			}
			seen[day] = true
			days = append(days, day)
		}
		if len(days) != 7 {
			continue
		}
		dayCells := directCells(rows[1])
		if len(dayCells) != 7 {
			return nil, fmt.Errorf("ders programı gün sütunları eksik")
		}
		var entries []Entry
		for dayIndex, dayCell := range dayCells {
			var meetingCells []*htmlnode.Node
			findMeetingCells(dayCell, &meetingCells)
			for _, cell := range meetingCells {
				lines := cellLines(cell)
				if len(lines) != 4 {
					return nil, fmt.Errorf("ders programında eksik ders alanı: gün=%d", dayIndex)
				}
				var start, end string
				if _, err := fmt.Sscanf(lines[3], "%5s - %5s", &start, &end); err != nil || !validClock(start) || !validClock(end) || start >= end {
					return nil, fmt.Errorf("ders programında geçersiz saat: %q", lines[3])
				}
				entries = append(entries, Entry{
					Day: days[dayIndex], Code: lines[0], Name: lines[1],
					Location: lines[2], Start: start, End: end,
					Online: isOnline(lines[2]),
				})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Day != entries[j].Day {
				return weekdayOrder(entries[i].Day) < weekdayOrder(entries[j].Day)
			}
			return entries[i].Start < entries[j].Start
		})
		return entries, nil
	}
	return nil, fmt.Errorf("ders programı tablosu bulunamadı; oturum kapanmış olabilir")
}

func FormatWeek(entries []Entry) string {
	var out strings.Builder
	out.WriteString("📅 <b>Ders Programım</b>\n")
	if len(entries) == 0 {
		out.WriteString("Bu dönemde programda ders görünmüyor.")
		return out.String()
	}
	for day := time.Monday; day <= time.Saturday; day++ {
		writeDay(&out, entries, day)
	}
	writeDay(&out, entries, time.Sunday)
	return strings.TrimSpace(out.String())
}

func writeDay(out *strings.Builder, entries []Entry, day time.Weekday) {
	var dayEntries []Entry
	for _, entry := range entries {
		if entry.Day == day {
			dayEntries = append(dayEntries, entry)
		}
	}
	out.WriteString("\n<b>" + dayNames[day] + "</b>\n")
	if len(dayEntries) == 0 {
		out.WriteString("— Ders yok\n")
		return
	}
	for _, entry := range dayEntries {
		icon := "🏫"
		location := entry.Location
		if entry.Online {
			icon = "🌐"
			location = "Online"
		}
		fmt.Fprintf(out, "%s–%s  %s <b>%s</b>\n     %s\n", entry.Start, entry.End, icon, html.EscapeString(entry.Name), html.EscapeString(location))
	}
}

func Due(entries []Entry, now time.Time, lead time.Duration, sent map[string]bool) []Reminder {
	var due []Reminder
	date := now.Format("2006-01-02")
	for _, entry := range entries {
		if entry.Day != now.Weekday() {
			continue
		}
		start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+entry.Start, now.Location())
		if err != nil || now.Before(start.Add(-lead)) || !now.Before(start) {
			continue
		}
		key := date + ":" + entry.Start + ":" + entry.Code + ":" + entry.Name
		if !sent[key] {
			due = append(due, Reminder{Entry: entry, Key: key})
		}
	}
	return due
}

func weekdayOrder(day time.Weekday) int { return (int(day) + 6) % 7 }

func validClock(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func isOnline(location string) bool {
	return strings.Contains(foldTurkish(location), "ONLINE")
}

func parseDay(label string) (time.Weekday, bool) {
	switch foldTurkish(label) {
	case "PAZARTESI":
		return time.Monday, true
	case "SALI":
		return time.Tuesday, true
	case "CARSAMBA":
		return time.Wednesday, true
	case "PERSEMBE":
		return time.Thursday, true
	case "CUMA":
		return time.Friday, true
	case "CUMARTESI":
		return time.Saturday, true
	case "PAZAR":
		return time.Sunday, true
	default:
		return 0, false
	}
}

func foldTurkish(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		switch r {
		case 'Ç', 'ç':
			return 'C'
		case 'Ğ', 'ğ':
			return 'G'
		case 'İ', 'ı':
			return 'I'
		case 'Ö', 'ö':
			return 'O'
		case 'Ş', 'ş':
			return 'S'
		case 'Ü', 'ü':
			return 'U'
		}
		return unicode.ToUpper(r)
	}, value)
}

func findElements(n *htmlnode.Node, tag string, out *[]*htmlnode.Node) {
	if n.Type == htmlnode.ElementNode && n.Data == tag {
		*out = append(*out, n)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		findElements(child, tag, out)
	}
}

func tableRows(table *htmlnode.Node) []*htmlnode.Node {
	var rows []*htmlnode.Node
	for child := table.FirstChild; child != nil; child = child.NextSibling {
		if child.Data == "tr" {
			rows = append(rows, child)
		} else if child.Data == "tbody" || child.Data == "thead" {
			for row := child.FirstChild; row != nil; row = row.NextSibling {
				if row.Data == "tr" {
					rows = append(rows, row)
				}
			}
		}
	}
	return rows
}

func directCells(row *htmlnode.Node) []*htmlnode.Node {
	var cells []*htmlnode.Node
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Data == "td" || child.Data == "th" {
			cells = append(cells, child)
		}
	}
	return cells
}

func findMeetingCells(n *htmlnode.Node, out *[]*htmlnode.Node) {
	if n.Data == "td" {
		for _, attr := range n.Attr {
			if attr.Key == "class" && strings.Contains(attr.Val, "bilgi_satir") {
				*out = append(*out, n)
				return
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		findMeetingCells(child, out)
	}
}

func cellLines(cell *htmlnode.Node) []string {
	var lines []string
	var current strings.Builder
	var walk func(*htmlnode.Node)
	walk = func(n *htmlnode.Node) {
		if n.Type == htmlnode.ElementNode && n.Data == "br" {
			lines = append(lines, strings.Join(strings.Fields(current.String()), " "))
			current.Reset()
			return
		}
		if n.Type == htmlnode.TextNode {
			current.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(cell)
	lines = append(lines, strings.Join(strings.Fields(current.String()), " "))
	return lines
}

func nodeText(n *htmlnode.Node) string {
	var out strings.Builder
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if node.Type == htmlnode.TextNode {
			out.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.TrimFunc(out.String(), unicode.IsSpace)
}
