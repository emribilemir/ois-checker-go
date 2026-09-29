package dersecme

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"notbot/config"
)

type dersecmeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn dersecmeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCheckFollowsCourseSelectionLinkAndRejectsEndedTargetPage(t *testing.T) {
	detailRequests := 0
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a></body></html>`
		if req.URL.Path == "/ogrenciler/derssecme/ogrindex" {
			detailRequests++
			body = `<html><body><font>DEĞERLİ ÖĞRENCİMİZ, DERS SEÇİMLERİ SONA ERMİŞTİR.</font></body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: req,
		}, nil
	})}

	found, keyword, err := Check(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected ended target page to be inactive, matched %q", keyword)
	}
	if detailRequests != 1 {
		t.Fatalf("expected the course-selection link to be fetched once, got %d", detailRequests)
	}
}

func TestCheckRejectsLoginPageReturnedAtCourseSelectionURL(t *testing.T) {
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body>
			<form method="POST" action="/auth/login/ln/tr">
				<input type="text" name="kullanici_adi">
				<input type="password" name="kullanici_sifre">
				<input type="text" name="captcha">
				<button type="submit">Giriş Yap</button>
			</form>
			<script>alertDiyalog("Bilgi","Oturumunuz farklı bir ekranda açıldı.","ok");</script>
		</body></html>`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	found, keyword, err := Check(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	})
	if err == nil || !strings.Contains(err.Error(), "session_expired") {
		t.Fatalf("expected a session_expired error for the login document, found=%v keyword=%q err=%v", found, keyword, err)
	}
}

func TestInspectExtractsCoursesFromSelectedCoursesTable(t *testing.T) {
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a></body></html>`
		if req.URL.Path == "/ogrenciler/derssecme/ogrindex" {
			body = `<html><body>
				<script>function dersiAl(){}; var url="/ogrenciler/derssecme/ogrderskaydet";</script>
				<table><tr><th>Öğrenci Bilgileri</th></tr><tr><td>240000000</td><td>Test Öğrenci</td></tr></table>
				<table class="table table-bordered table-striped">
					<tr><th colspan="8">Seçtiğiniz Dersler</th><th colspan="4">Yerine Sayılacak Dersler</th></tr>
					<tr><th>Ders Kodu</th><th>Ders Adı</th><th>Kredi</th><th>AKTS</th></tr>
					<tr><td>SEC101</td><td>Kolay Seçmeli</td><td>3</td><td>5</td><td><input type="button" value="Dersi Sil" onclick="dersiSil(1,2,0)"></td></tr>
					<tr><td>MAT202</td><td>Matematik II</td><td>3</td><td>5</td><td><input type="button" value="Dersi Sil" onclick="dersiSil(3,4,0)"></td></tr>
					<tr><td colspan="100"><b>AKTS Limiti:</b> 30</td></tr>
				</table>
				<table class="dersler"><tr><th>Daha Önce Almadığınız Dersler</th></tr><tr><td>OTHER1</td><td>Seçilmemiş Ders</td></tr></table>
			</body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	status, err := Inspect(client, &config.Config{UniversityURL: "https://ois.example", UserAgent: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Open {
		t.Fatal("expected the course-selection page to be open")
	}
	want := []SelectedCourse{{Code: "SEC101", Name: "Kolay Seçmeli"}, {Code: "MAT202", Name: "Matematik II"}}
	if len(status.SelectedCourses) != len(want) {
		t.Fatalf("selected courses = %#v, want %#v", status.SelectedCourses, want)
	}
	for i := range want {
		if status.SelectedCourses[i] != want[i] {
			t.Fatalf("selected course %d = %#v, want %#v", i, status.SelectedCourses[i], want[i])
		}
	}
}

func TestInspectKeepsAnEmptySelectedCoursesTableObservable(t *testing.T) {
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a></body></html>`
		if req.URL.Path == "/ogrenciler/derssecme/ogrindex" {
			body = `<html><body>
				<script>function dersiAl(){}; var url="/ogrenciler/derssecme/ogrderskaydet";</script>
				<table><tr><th>Seçtiğiniz Dersler</th></tr><tr><th>Ders Kodu</th><th>Ders Adı</th></tr></table>
			</body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	status, err := Inspect(client, &config.Config{UniversityURL: "https://ois.example", UserAgent: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SelectedCourses == nil {
		t.Fatal("expected an empty but available selected-course list so removals remain detectable")
	}
	if len(status.SelectedCourses) != 0 {
		t.Fatalf("expected no selected courses, got %#v", status.SelectedCourses)
	}
}

func TestInspectDiscoversElectivePoolLinks(t *testing.T) {
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a>`
		if req.URL.Path == "/ogrenciler/derssecme/ogrindex" {
			body = `<script>function dersiAl(){}; var url="/ogrenciler/derssecme/ogrderskaydet";</script>` +
				`<input value="Ders Seç" onclick="popUp2('/ogrenciler/derssecme/popderssecme/havuz_id/319/ogrenci_slot_id/555925',800,800)">`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	status, err := Inspect(client, &config.Config{UniversityURL: "https://ois.example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.PoolPaths) != 1 || status.PoolPaths[0] != "/ogrenciler/derssecme/popderssecme/havuz_id/319/ogrenci_slot_id/555925" {
		t.Fatalf("pool link not discovered: %#v", status.PoolPaths)
	}
}

func TestSearchKeywordsIgnoresEndedCourseSelectionNotice(t *testing.T) {
	body := []byte(`<font size="5" color="red">DEĞERLİ ÖĞRENCİMİZ, DERS SEÇİMLERİ SONA ERMİŞTİR, DERS SEÇME İŞLEMİ İÇİN DANIŞMANINIZLA İLETİŞİME GEÇİNİZ.</font>`)

	found, keyword, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected ended notice to be ignored, matched %q", keyword)
	}
}

func TestSearchKeywordsIgnoresCourseSelectionClosedForStudentClass(t *testing.T) {
	body := []byte(`<main><h2>Ders Seçme</h2><div style="color: red">Sizin sınıfınız için ders seçme işlemleri kapalı</div></main>`)

	found, keyword, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected the class-specific closed notice to be inactive, matched %q", keyword)
	}
}

func TestSearchKeywordsEndedNoticeOverridesPersistentCourseSelectionMenuLink(t *testing.T) {
	body := []byte(`<nav><a href="/ogrenciler/ders-secme">Ders Seçme</a></nav>` +
		`<main>` + strings.Repeat("duyuru içeriği ", 40) +
		`<strong>DEĞERLİ ÖĞRENCİMİZ, DERS SEÇİMLERİ SONA ERMİŞTİR.</strong></main>`)

	found, keyword, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected the ended notice to override the persistent menu link, matched %q", keyword)
	}
}

func TestSearchKeywordsDoesNotTreatPersistentMenuLinkAsOpenRegistration(t *testing.T) {
	body := []byte(`<nav><a href="/ogrenciler/ders-secme">Ders Seçme</a></nav>`)

	found, keyword, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected a permanent navigation link not to count as open registration, matched %q", keyword)
	}
}

func TestSearchKeywordsFindsRealCourseSelectionControls(t *testing.T) {
	body := []byte(`<html><body>
		<div class="new-breadcrumb">Ders Seçme</div>
		<table><tr><th>Seçtiğiniz Dersler</th></tr>
		<tr><td><input type="button" value="Dersi Sil" onclick="dersiSil(934535,33648,0)"></td></tr></table>
		<script>function dersiAl() { var url="/ogrenciler/derssecme/ogrderskaydet"; }</script>
	</body></html>`)

	found, keyword, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected real course-selection controls to be recognized")
	}
	if keyword == "" {
		t.Fatal("expected an active-page signal")
	}
}

func TestSearchKeywordsFindsCourseSelectionFromAttributes(t *testing.T) {
	body := []byte(`<button onclick="location.href='/ogrenciler/dersKayit'">Başvuru</button>`)

	found, _, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected course registration signal from onclick attribute")
	}
}

func TestSearchKeywordsNormalizesTurkishCharacters(t *testing.T) {
	body := []byte(`<div title="KAYIT YENİLEME">Öğrenci işlemleri</div>`)

	found, _, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected normalized Turkish keyword to be found")
	}
}
