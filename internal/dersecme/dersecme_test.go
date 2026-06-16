package dersecme

import "testing"

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
