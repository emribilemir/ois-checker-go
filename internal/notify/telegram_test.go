package notify

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
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
