package exams

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"notbot/config"
)

func TestParseSeparateDateAndTimeColumns(t *testing.T) {
	body := []byte(`<html><body><table class="a4">
		<tr><th>Ders Kodu</th><th>Ders Adı</th><th>Sınav Türü</th><th>Sınav Tarihi</th><th>Başlangıç Saati</th><th>Bitiş Saati</th><th>Derslik</th></tr>
		<tr><td>BLM305</td><td>Veritabanı Yönetimi</td><td>Final</td><td>05.01.2027</td><td>9:30</td><td>11:00</td><td>A-201</td></tr>
		<tr><td>BLM301</td><td>İşletim Sistemleri</td><td>Ara Sınav</td><td>11.11.2026</td><td>13:00</td><td>14:00</td><td>B-105</td></tr>
		<tr><td colspan="7">Toplam 2 sınav</td></tr>
	</table></body></html>`)

	exams, _, ok := Parse(body)
	if !ok || len(exams) != 2 {
		t.Fatalf("expected two exams, ok=%v exams=%#v", ok, exams)
	}
	first := exams[0]
	if first.Code != "BLM301" || first.Name != "İşletim Sistemleri" || first.Kind != "Ara Sınav" || first.Start != "13:00" || first.End != "14:00" || first.Place != "B-105" {
		t.Fatalf("unexpected first exam (should be sorted by date): %#v", first)
	}
	if exams[1].Start != "09:30" || !exams[1].Date.Equal(time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected second exam: %#v", exams[1])
	}
}

func TestParseCombinedDateTimeAndCodeInCourseCell(t *testing.T) {
	body := []byte(`<table>
		<thead><tr><th>Ders</th><th>Sınav</th><th>Tarih / Saat</th><th>Sınav Yeri</th></tr></thead>
		<tbody><tr><td>BLM 307 - Robotik</td><td>Vize</td><td>2026-11-20 10:00 - 11:30</td><td>Online</td></tr></tbody>
	</table>`)

	exams, _, ok := Parse(body)
	if !ok || len(exams) != 1 {
		t.Fatalf("expected one exam, ok=%v exams=%#v", ok, exams)
	}
	exam := exams[0]
	if exam.Code != "BLM 307" || exam.Name != "Robotik" || exam.Kind != "Vize" || exam.Start != "10:00" || exam.End != "11:30" || exam.Place != "Online" {
		t.Fatalf("unexpected exam: %#v", exam)
	}
}

func TestParseReportsHeadersOfUnrecognisedTables(t *testing.T) {
	body := []byte(`<table><tr><th>Etki Oranı</th><th>BLM301 - Ders</th><th>Puan</th><th>Açıklanma Tarihi</th></tr><tr><td>%40</td><td>Vize</td><td>80</td><td>15/11/2026</td></tr></table>`)

	exams, headers, ok := Parse(body)
	if ok || len(exams) != 0 {
		t.Fatalf("grade table must not be read as an exam calendar: %#v", exams)
	}
	if len(headers) != 1 || !strings.Contains(headers[0], "Puan") {
		t.Fatalf("expected the table headers for diagnostics, got %#v", headers)
	}
}

func TestFetchFollowsMenuLinkAndSkipsGradeResults(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<ul>
				<li><a href="/ogrenciler/belge/ogrsinavsonuc">Sınav Sonuçları</a></li>
				<li><a href="javascript:void(0)" onclick="popUp2('/ogrenciler/belge/sinavyerlerim')">SINAV PROGRAMI</a></li>
				<li><a href="https://baska.example/sinavprogrami">Sınav Programı (dış)</a></li>
			</ul>`))
		case "/ogrenciler/belge/sinavyerlerim":
			w.Write([]byte(`<table><tr><th>Ders Adı</th><th>Tarih</th><th>Saat</th></tr><tr><td>Robotik</td><td>20/11/2026</td><td>10:00</td></tr></table>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	exams, err := Fetch(server.Client(), &config.Config{UniversityURL: server.URL})
	if err != nil || len(exams) != 1 || exams[0].Name != "Robotik" {
		t.Fatalf("expected the exam from the menu page, exams=%#v err=%v", exams, err)
	}
	for _, request := range requested {
		if !strings.HasPrefix(request, "GET ") || strings.Contains(request, "sinavsonuc") {
			t.Fatalf("unexpected OIS request %q in %v", request, requested)
		}
	}
}

func TestFetchExplainsWhatItSawWhenCalendarIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<a href="/ogrenciler/ders">Derslerim</a>`))
		case "/ogrenciler/belge/sinavprogrami":
			w.Write([]byte(`<table><tr><th>Dönem</th><th>Açıklama</th></tr></table>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := Fetch(server.Client(), &config.Config{UniversityURL: server.URL})
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	for _, want := range []string{"Menüde sınav takvimi bağlantısı bulunamadı", "/ogrenciler/belge/sinavprogrami: tablo tanınmadı, başlıklar: Dönem, Açıklama"} {
		if !strings.Contains(message, want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, message)
		}
	}
}

func TestFetchReportsExpiredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/login" {
			w.Write([]byte(`<form action="/auth/login"><input name="kullanici_sifre"></form>`))
			return
		}
		http.Redirect(w, r, "/auth/login", http.StatusFound)
	}))
	defer server.Close()

	_, err := Fetch(server.Client(), &config.Config{UniversityURL: server.URL})
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

func TestFormatShowsUpcomingExamsGroupedByDay(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	now := time.Date(2026, 11, 13, 22, 0, 0, 0, time.FixedZone("Istanbul", 3*3600))
	message := Format([]Exam{
		{Name: "Eski", Date: day(10), Start: "10:00"},
		{Name: "Robotik", Kind: "Vize", Date: day(14), Start: "10:00", End: "11:30", Place: "A<1>"},
		{Name: "Ağlar", Date: day(14), Start: "13:00"},
		{Code: "BLM305", Date: day(20)},
	}, now)

	for _, want := range []string{"14 Kasım Cumartesi</b> · yarın", "🕒 10:00–11:30  <b>Robotik</b> (Vize)", "📍 A&lt;1&gt;", "🕒 13:00  <b>Ağlar</b>", "20 Kasım Cuma</b> · 7 gün sonra", "Saat belirtilmemiş  <b>BLM305</b>", "Geçmiş 1 sınav gösterilmedi"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message missing %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, "Eski") || strings.Count(message, "14 Kasım") != 1 {
		t.Fatalf("past exam shown or day header repeated:\n%s", message)
	}
}

func TestFormatEmptyCalendar(t *testing.T) {
	if message := Format(nil, time.Now()); !strings.Contains(message, "henüz sınav yok") {
		t.Fatalf("unexpected empty message: %s", message)
	}
}

func TestFetchReadsTheKnownCalendarPageWithoutVisitingOtherPages(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/ogrenciler/belge/sinavprogrami" {
			w.Write([]byte(`<table><tr><th>Ders Adı</th><th>Sınav Tarihi</th><th>Saat</th><th>Derslik</th></tr><tr><td>Robotik</td><td>27.11.2026</td><td>10:00</td><td>A-1</td></tr></table>`))
			return
		}
		http.Redirect(w, r, "/auth/logout", http.StatusFound)
	}))
	defer server.Close()

	exams, err := Fetch(server.Client(), &config.Config{UniversityURL: server.URL})
	if err != nil || len(exams) != 1 || exams[0].Place != "A-1" {
		t.Fatalf("expected the calendar page, exams=%#v err=%v", exams, err)
	}
	if len(requested) != 1 {
		t.Fatalf("a readable calendar must need one request, got %v", requested)
	}
}

func TestFetchTreatsLogoutRedirectAsExpiredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/logout" {
			w.Write([]byte(`çıkış`))
			return
		}
		http.Redirect(w, r, "/auth/logout", http.StatusFound)
	}))
	defer server.Close()

	_, err := Fetch(server.Client(), &config.Config{UniversityURL: server.URL})
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

// atlasPage, Atlas OİS sınav programı sayfasının düzenini izler. Kimlik değerleri sahtedir.
func atlasPage(rows string) []byte {
	return []byte(`<html><body>
		<h3>SINAV PROGRAMI</h3>
		<table>
			<tr><th>TC Kimlik No</th><td>00000000000</td><th>Fakülte</th><td>05-MÜHENDİSLİK VE DOĞA BİLİMLERİ FAKÜLTESİ</td></tr>
			<tr><th>Adı</th><td>DENEME</td><th>Bölüm/Program</th><td>0501-BİLGİSAYAR MÜHENDİSLİĞİ BÖLÜMÜ</td></tr>
			<tr><th>Soyadı</th><td>ÖĞRENCİ</td><th>Akademik Yıl</th><td>2026-2027</td></tr>
			<tr><th>Sınıfı</th><td>3</td><th>Dönem</th><td>Güz</td></tr>
		</table>
		<table>
			<tr><th>SINAV TİPİ</th><th>DERS KODU</th><th>DERS ADI</th><th>TARİH - SAAT</th><th>KAMPÜS</th><th>DERSLİK</th><th>ÖĞRETİM ELEMANI</th><th>Açıklama</th></tr>
			` + rows + `
		</table>
	</body></html>`)
}

func TestParseAtlasExamProgramme(t *testing.T) {
	exams, _, ok := Parse(atlasPage(`
		<tr><td>Final</td><td>BLM305</td><td>VERİTABANI YÖNETİM SİSTEMLERİ</td><td>05.01.2027 10:00</td><td>Kağıthane</td><td>A-201</td><td>Dr. Öğr. Üyesi Deneme</td><td>Kitap kapalı</td></tr>
		<tr><td>Ara Sınav</td><td>BLM307</td><td>ROBOTİK</td><td>20.11.2026 13:30</td><td>Kağıthane</td><td>B-105</td><td>Prof. Dr. Deneme</td><td></td></tr>`))
	if !ok || len(exams) != 2 {
		t.Fatalf("expected two exams, ok=%v exams=%#v", ok, exams)
	}
	first := exams[0]
	if first.Kind != "Ara Sınav" || first.Code != "BLM307" || first.Name != "ROBOTİK" || first.Start != "13:30" ||
		first.Campus != "Kağıthane" || first.Place != "B-105" || first.Instructor != "Prof. Dr. Deneme" {
		t.Fatalf("unexpected first exam: %#v", first)
	}
	if exams[1].Note != "Kitap kapalı" || !exams[1].Date.Equal(time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected second exam: %#v", exams[1])
	}

	message := Format(exams, time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{"🕒 13:30  <b>ROBOTİK</b> (Ara Sınav)", "📍 B-105 · Kağıthane", "👤 Prof. Dr. Deneme", "ℹ️ Kitap kapalı"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message missing %q:\n%s", want, message)
		}
	}
}

func TestParseEmptyAtlasExamProgramme(t *testing.T) {
	exams, _, ok := Parse(atlasPage(""))
	if !ok || len(exams) != 0 {
		t.Fatalf("an empty programme is a valid calendar, ok=%v exams=%#v", ok, exams)
	}
	if message := Format(exams, time.Now()); !strings.Contains(message, "henüz sınav yok") {
		t.Fatalf("unexpected message: %s", message)
	}
}

func TestDiagnosticsNeverIncludeStudentDetails(t *testing.T) {
	page := strings.Replace(string(atlasPage("")), "TARİH - SAAT", "ZAMAN", 1)
	_, headers, ok := Parse([]byte(page))
	if ok {
		t.Fatal("a table without a date column must not be recognised")
	}
	joined := strings.Join(headers, " | ")
	for _, private := range []string{"00000000000", "DENEME", "ÖĞRENCİ", "2026-2027"} {
		if strings.Contains(joined, private) {
			t.Fatalf("diagnostics leaked %q: %s", private, joined)
		}
	}
	if !strings.Contains(joined, "SINAV TİPİ") {
		t.Fatalf("expected exam table headers in diagnostics, got %s", joined)
	}
}
