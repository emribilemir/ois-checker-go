package notify

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestScheduleMessageUsesHTMLAndReminderControls(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if values.Get("parse_mode") != "HTML" || !strings.Contains(values.Get("reply_markup"), "cmd_schedule") || !strings.Contains(values.Get("reply_markup"), "cmd_reminders_off") {
			t.Errorf("schedule message missing HTML mode or controls: %v", values)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	if err := SendSchedule("token", "123", "<b>Program</b>", false, false, true); err != nil {
		t.Fatal(err)
	}
}

func TestSendTelegramSetsARequestDeadline(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if _, ok := req.Context().Deadline(); !ok {
			return nil, errors.New("telegram request has no deadline")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	if err := SendTelegram("token", "123", "message"); err != nil {
		t.Fatalf("expected a bounded Telegram request, got %v", err)
	}
}
