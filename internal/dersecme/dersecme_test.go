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

func TestSearchKeywordsFindsActiveCourseSelectionLink(t *testing.T) {
	body := []byte(`<nav><a href="/ogrenciler/ders-secme">Ders Seçme</a></nav>`)

	found, keyword, err := searchKeywords(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected active course selection link to be found")
	}
	if keyword == "" {
		t.Fatal("expected matched keyword")
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
