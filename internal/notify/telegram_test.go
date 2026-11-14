package notify

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type recordedCall struct {
	Method  string
	Payload map[string]any
}

// fakeTelegram answers each API method with the given status; unknown methods succeed.
func fakeTelegram(t *testing.T, statuses map[string]int, descriptions map[string]string) *[]recordedCall {
	t.Helper()
	var calls []recordedCall
	previous := http.DefaultTransport
	http.DefaultTransport = telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if _, ok := req.Context().Deadline(); !ok {
			return nil, errors.New("telegram request has no deadline")
		}
		method := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		var payload map[string]any
		body, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("%s payload is not JSON: %v", method, err)
		}
		calls = append(calls, recordedCall{method, payload})
		status := http.StatusOK
		if s, ok := statuses[method]; ok {
			status = s
		}
		response := `{"ok":true,"result":{}}`
		if status != http.StatusOK {
			response = `{"ok":false,"description":` + mustJSON(descriptions[method]) + `}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	return &calls
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var sample = Message{
	Rich:     "<h3>Başlık</h3><table><tr><td>a</td></tr></table>",
	Plain:    "<b>Başlık</b>\n<blockquote>a</blockquote>",
	Keyboard: Keyboard{{{Text: "Menü", Data: "cmd_menu"}, {Text: "OİS", URL: "https://ois.example"}}},
	Silent:   true,
}

func TestSendUsesRichMessageWithKeyboard(t *testing.T) {
	calls := fakeTelegram(t, nil, nil)
	if err := Send("token", "123", sample); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].Method != "sendRichMessage" {
		t.Fatalf("expected one sendRichMessage call, got %#v", *calls)
	}
	payload := (*calls)[0].Payload
	rich := payload["rich_message"].(map[string]any)
	if rich["html"] != sample.Rich || rich["skip_entity_detection"] != true || payload["disable_notification"] != true || payload["chat_id"] != "123" {
		t.Fatalf("unexpected rich payload: %#v", payload)
	}
	keyboard, _ := json.Marshal(payload["reply_markup"])
	if !strings.Contains(string(keyboard), `"callback_data":"cmd_menu"`) || !strings.Contains(string(keyboard), `"url":"https://ois.example"`) {
		t.Fatalf("unexpected keyboard: %s", keyboard)
	}
}

func TestSendFallsBackToPlainHTMLWhenRichIsRejected(t *testing.T) {
	calls := fakeTelegram(t, map[string]int{"sendRichMessage": http.StatusBadRequest}, map[string]string{"sendRichMessage": "Bad Request: can't parse rich message"})
	if err := Send("token", "123", sample); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[1].Method != "sendMessage" {
		t.Fatalf("expected rich then plain, got %#v", *calls)
	}
	plain := (*calls)[1].Payload
	if plain["text"] != sample.Plain || plain["parse_mode"] != "HTML" || plain["reply_markup"] == nil {
		t.Fatalf("unexpected plain payload: %#v", plain)
	}
}

func TestSendDoesNotDuplicateWhenTelegramIsDown(t *testing.T) {
	calls := fakeTelegram(t, map[string]int{"sendRichMessage": http.StatusBadGateway}, nil)
	if err := Send("token", "123", sample); err == nil {
		t.Fatal("expected the server error to be returned")
	}
	if len(*calls) != 1 {
		t.Fatalf("a server error must not trigger the plain fallback, got %#v", *calls)
	}
}

func TestSendPlainOnlyMessage(t *testing.T) {
	calls := fakeTelegram(t, nil, nil)
	if err := Send("token", "123", Message{Plain: "metin"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].Method != "sendMessage" || (*calls)[0].Payload["reply_markup"] != nil {
		t.Fatalf("expected one plain message without keyboard, got %#v", *calls)
	}
}

func TestEditTreatsUnchangedMessageAsSuccess(t *testing.T) {
	calls := fakeTelegram(t, map[string]int{"editMessageText": http.StatusBadRequest}, map[string]string{"editMessageText": "Bad Request: message is not modified"})
	if err := Edit("token", "123", 42, sample); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].Payload["message_id"] != float64(42) || (*calls)[0].Payload["rich_message"] == nil {
		t.Fatalf("expected one rich edit, got %#v", *calls)
	}
}

func TestEditSendsNewMessageWhenOriginalCannotBeEdited(t *testing.T) {
	calls := fakeTelegram(t, map[string]int{"editMessageText": http.StatusBadRequest}, map[string]string{"editMessageText": "Bad Request: message to edit not found"})
	if err := Edit("token", "123", 42, sample); err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, c := range *calls {
		methods = append(methods, c.Method)
	}
	if strings.Join(methods, ",") != "editMessageText,editMessageText,sendRichMessage" {
		t.Fatalf("expected rich edit, plain edit, then new message; got %v", methods)
	}
}

func TestCommandNameAcceptsMenuCommands(t *testing.T) {
	for text, want := range map[string]string{"/sinav": "/sinav", "/Sinavlar@NotBot": "/sinavlar", "/program": "/program", "merhaba": "merhaba"} {
		if got := commandName(text); got != want {
			t.Fatalf("commandName(%q) = %q, want %q", text, got, want)
		}
	}
	for _, accepted := range []string{"/start", "/menu", "/notlar", "/program", "/sinav", "/sinavlar@NotBot", "/ayarlar"} {
		if !isCommand(accepted) {
			t.Fatalf("%s must be accepted", accepted)
		}
	}
	if isCommand("/sil") {
		t.Fatal("unknown commands must be ignored")
	}
}

func TestNetworkErrorsDoNotLeakTheBotToken(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	err := Send("123456:SECRET-TOKEN", "1", Message{Plain: "x"})
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkipBacklogStartsAfterTheLastPendingUpdate(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`{"ok":true,"result":[{"update_id":4711}]}`))
	}))
	defer server.Close()
	if got := skipBacklog(server.URL); got != 4712 || !strings.Contains(query, "offset=-1") {
		t.Fatalf("offset=%d query=%q", got, query)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer empty.Close()
	if got := skipBacklog(empty.URL); got != 0 {
		t.Fatalf("no backlog must start at 0, got %d", got)
	}
}
