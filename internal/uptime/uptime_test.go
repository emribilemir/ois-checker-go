package uptime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRobot struct {
	status int
	edits  []string
	key    string
}

func (f *fakeRobot) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		f.key = r.Form.Get("api_key")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMonitors"):
			w.Write([]byte(`{"stat":"ok","monitors":[{"id":1,"url":"https://baska.example/","status":2},{"id":42,"url":"https://ois-checker-go.onrender.com/","status":` + map[int]string{0: "0", 2: "2", 9: "9"}[f.status] + `}]}`))
		case strings.HasSuffix(r.URL.Path, "/editMonitor"):
			f.edits = append(f.edits, r.Form.Get("id")+"="+r.Form.Get("status"))
			w.Write([]byte(`{"stat":"ok","monitor":{"id":42}}`))
		default:
			w.Write([]byte(`{"stat":"fail","error":{"type":"not_found","message":"yok"}}`))
		}
	}))
}

func TestEnsurePausesAndResumesOnlyWhenNeeded(t *testing.T) {
	robot := &fakeRobot{status: 2}
	srv := robot.server(t)
	defer srv.Close()
	c := &Client{APIKey: "k", MonitorURL: "https://ois-checker-go.onrender.com", Base: srv.URL, HTTP: srv.Client()}

	if changed, err := c.Ensure(true); err != nil || changed || len(robot.edits) != 0 {
		t.Fatalf("an active monitor must be left alone: changed=%v err=%v edits=%v", changed, err, robot.edits)
	}
	if changed, err := c.Ensure(false); err != nil || !changed || robot.edits[0] != "42=0" {
		t.Fatalf("expected a pause of monitor 42: changed=%v err=%v edits=%v", changed, err, robot.edits)
	}
	robot.status = 0
	if changed, err := c.Ensure(true); err != nil || !changed || robot.edits[1] != "42=1" {
		t.Fatalf("expected a resume of monitor 42: changed=%v err=%v edits=%v", changed, err, robot.edits)
	}
	if robot.key != "k" {
		t.Fatal("api key not sent")
	}
}

func TestEnsureExplainsAMissingMonitorAndIsOffWithoutKey(t *testing.T) {
	robot := &fakeRobot{status: 2}
	srv := robot.server(t)
	defer srv.Close()
	c := &Client{APIKey: "k", MonitorURL: "https://baska-bot.onrender.com/", Base: srv.URL, HTTP: srv.Client()}
	if _, err := c.Ensure(false); err == nil || !strings.Contains(err.Error(), "bulunamadı") {
		t.Fatalf("expected a missing-monitor error, got %v", err)
	}
	if changed, err := (&Client{}).Ensure(false); changed || err != nil {
		t.Fatal("a client without a key must do nothing")
	}
}
