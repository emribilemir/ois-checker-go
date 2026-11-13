package settings

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type call struct {
	method  string
	payload map[string]any
}

// fakeTelegram answers getChat with pinned (or no pinned message when empty) and
// fails methods listed in failing with the given status.
func fakeTelegram(t *testing.T, pinnedID int, pinnedText string, failing map[string]int) *[]call {
	t.Helper()
	var calls []call
	previous := http.DefaultTransport
	http.DefaultTransport = roundTrip(func(req *http.Request) (*http.Response, error) {
		method := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		var payload map[string]any
		body, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(body, &payload)
		calls = append(calls, call{method, payload})
		status, response := http.StatusOK, `{"ok":true,"result":true}`
		switch {
		case failing[method] != 0:
			status, response = failing[method], `{"ok":false,"description":"Bad Request: message to edit not found"}`
		case method == "getChat" && pinnedID != 0:
			pinned, _ := json.Marshal(map[string]any{"message_id": pinnedID, "text": pinnedText})
			response = `{"ok":true,"result":{"id":1,"pinned_message":` + string(pinned) + `}}`
		case method == "getChat":
			response = `{"ok":true,"result":{"id":1}}`
		case method == "sendMessage":
			response = `{"ok":true,"result":{"message_id":77}}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	return &calls
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	for _, s := range []Settings{{}, {Paused: true}, {CourseSelection: true, Reminders: true}, {Paused: true, CourseSelection: true, Reminders: true}} {
		got, ok := Decode(Encode(s))
		if !ok || got != s {
			t.Fatalf("round trip %+v -> %+v ok=%v\n%s", s, got, ok, Encode(s))
		}
	}
	if _, ok := Decode("Toplantı notları\n✅ Not taraması"); ok {
		t.Fatal("a foreign pinned message must not be read as settings")
	}
	if _, ok := Decode("⚙️ Bot ayarları\n✅ Not taraması"); ok {
		t.Fatal("an incomplete settings message must be rejected")
	}
}

func TestLoadReadsThePinnedSettingsMessage(t *testing.T) {
	want := Settings{Paused: false, CourseSelection: false, Reminders: true}
	calls := fakeTelegram(t, 41, Encode(want), nil)
	st := &Store{Token: "t", ChatID: "123"}
	got, ok, err := st.Load()
	if err != nil || !ok || got != want {
		t.Fatalf("got %+v ok=%v err=%v", got, ok, err)
	}
	if err := st.Save(Settings{CourseSelection: true}); err != nil {
		t.Fatal(err)
	}
	last := (*calls)[len(*calls)-1]
	if last.method != "editMessageText" || last.payload["message_id"] != float64(41) || !strings.Contains(last.payload["text"].(string), "✅ Ders seçme takibi") {
		t.Fatalf("expected the pinned message to be edited, got %#v", last)
	}
}

func TestLoadIgnoresOtherPinnedMessages(t *testing.T) {
	fakeTelegram(t, 5, "Önemli link", nil)
	if _, ok, err := (&Store{Token: "t", ChatID: "123"}).Load(); ok || err != nil {
		t.Fatalf("foreign pinned message accepted: ok=%v err=%v", ok, err)
	}
}

func TestFirstSaveSendsAndPinsSilently(t *testing.T) {
	calls := fakeTelegram(t, 0, "", nil)
	st := &Store{Token: "t", ChatID: "123"}
	if _, ok, _ := st.Load(); ok {
		t.Fatal("no pinned message expected")
	}
	if err := st.Save(Settings{Reminders: true}); err != nil {
		t.Fatal(err)
	}
	methods := []string{}
	for _, c := range (*calls)[1:] {
		methods = append(methods, c.method)
	}
	if strings.Join(methods, ",") != "sendMessage,pinChatMessage" || (*calls)[2].payload["message_id"] != float64(77) || (*calls)[2].payload["disable_notification"] != true {
		t.Fatalf("expected a silent send and pin, got %#v", (*calls)[1:])
	}
	// The next save edits the new message instead of pinning another one.
	if err := st.Save(Settings{}); err != nil {
		t.Fatal(err)
	}
	if last := (*calls)[len(*calls)-1]; last.method != "editMessageText" || last.payload["message_id"] != float64(77) {
		t.Fatalf("expected an edit of message 77, got %#v", last)
	}
}

func TestSaveRepinsWhenTheSettingsMessageWasDeleted(t *testing.T) {
	calls := fakeTelegram(t, 41, Encode(Settings{}), map[string]int{"editMessageText": http.StatusBadRequest})
	st := &Store{Token: "t", ChatID: "123"}
	st.Load()
	if err := st.Save(Settings{Paused: true}); err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, c := range *calls {
		methods = append(methods, c.method)
	}
	if strings.Join(methods, ",") != "getChat,editMessageText,sendMessage,pinChatMessage" {
		t.Fatalf("unexpected calls %v", methods)
	}
}

func TestSaveDoesNotDuplicateWhenTelegramIsDown(t *testing.T) {
	calls := fakeTelegram(t, 41, Encode(Settings{}), map[string]int{"editMessageText": http.StatusBadGateway})
	st := &Store{Token: "t", ChatID: "123"}
	st.Load()
	if err := st.Save(Settings{Paused: true}); err == nil {
		t.Fatal("expected the outage to be reported")
	}
	if len(*calls) != 2 {
		t.Fatalf("an outage must not create a second settings message: %#v", *calls)
	}
}
