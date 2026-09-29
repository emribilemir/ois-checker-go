package dersecme

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"notbot/config"
)

func TestFetchPoolReadsCoursesAndQuotaWithoutEnrollment(t *testing.T) {
	const path = "/ogrenciler/derssecme/popderssecme/havuz_id/319/ogrenci_slot_id/555925"
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != path {
			t.Fatalf("unexpected OIS request: %s %s", req.Method, req.URL)
		}
		body := `<html><body><table><thead><tr><th>Ders Kodu</th><th>Ders Adı</th><th>Kredi</th><th>AKTS</th><th>Kalan Kota</th><th></th></tr></thead><tbody>
			<tr><td class="left">1410002054</td><td class="left">Siber Güvenlik</td><td>3</td><td>5</td><td class="kota">71</td><td><input type="button" value="Dersi Al" onclick="dersiAl('555925',33666)"></td></tr>
			<tr><td class="left">1410002068</td><td class="left">Robotiğe Giriş</td><td>3</td><td>5</td><td class="kota">0</td><td><input type="button" value="Dersi Al" onclick="dersiAl('555925',33680)"></td></tr>
		</tbody></table></body></html>`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	courses, err := FetchPool(client, &config.Config{UniversityURL: "https://ois.example", UserAgent: "test-agent"}, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(courses) != 2 || courses[0].Code != "1410002054" || courses[0].Name != "Siber Güvenlik" || courses[0].Quota != 71 || courses[1].Quota != 0 {
		t.Fatalf("unexpected pool courses: %#v", courses)
	}
}

func TestNewlyAvailableAlertsOnlyWhenCourseBecomesAvailable(t *testing.T) {
	before := []PoolCourse{{Code: "A", Name: "Dolu", Quota: 0}, {Code: "B", Name: "Açık", Quota: 5}}
	after := []PoolCourse{{Code: "A", Name: "Dolu", Quota: 2}, {Code: "B", Name: "Açık", Quota: 7}, {Code: "C", Name: "Yeni", Quota: 3}, {Code: "D", Name: "Yeni dolu", Quota: 0}}
	got := NewlyAvailable(before, after)
	if len(got) != 2 || got[0].Code != "A" || got[1].Code != "C" {
		t.Fatalf("expected reopened and new available courses, got %#v", got)
	}
}

func TestFetchPoolRejectsOffsiteAndMutationPaths(t *testing.T) {
	client := &http.Client{Transport: dersecmeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("invalid path caused request: %s", req.URL)
		return nil, nil
	})}
	cfg := &config.Config{UniversityURL: "https://ois.example"}
	for _, path := range []string{"https://other.example/ogrenciler/derssecme/popderssecme/havuz_id/319/ogrenci_slot_id/555925", "/ogrenciler/derssecme/ogrderskaydet"} {
		if _, err := FetchPool(client, cfg, path); err == nil {
			t.Fatalf("expected unsafe path %q to be rejected", path)
		}
	}
}
