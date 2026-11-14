package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"notbot/internal/telemetry"
)

var (
	telegramSendClient = &http.Client{Timeout: 15 * time.Second}
	telegramPollClient = &http.Client{Timeout: 40 * time.Second}
	apiBase            = "https://api.telegram.org"
)

// Button bir satır içi düğmedir. Data doluysa geri çağrı, URL doluysa bağlantı düğmesidir.
type Button struct {
	Text string
	Data string
	URL  string
}

// Keyboard satır satır düğmelerdir.
type Keyboard [][]Button

// Message bir bot mesajıdır. Rich, Telegram'ın zengin mesaj HTML'idir (başlık,
// tablo, açılır bölüm). Plain, zengin mesaj kabul edilmezse gönderilen sade
// Telegram HTML'idir ve her zaman doludur.
type Message struct {
	Rich     string
	Plain    string
	Keyboard Keyboard
	Silent   bool
}

// APIError Telegram'ın reddettiği bir isteği anlatır.
type APIError struct {
	Method      string
	Status      int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s %d: %s", e.Method, e.Status, e.Description)
}

// rejected, isteğin biçim yüzünden reddedildiğini söyler (ağ ya da sunucu hatası değil).
func rejected(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != http.StatusTooManyRequests
}

// IsRejected, Telegram'ın isteği biçim ya da içerik yüzünden reddettiğini söyler.
func IsRejected(err error) bool { return rejected(err) }

func notModified(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && strings.Contains(apiErr.Description, "message is not modified")
}

func call(token, method string, payload map[string]any) error {
	return callResult(token, method, payload, nil)
}

// callResult, Telegram'ın döndürdüğü "result" alanını out'a çözer (out nil olabilir).
func callResult(token, method string, payload map[string]any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := telegramSendClient.Post(fmt.Sprintf("%s/bot%s/%s", apiBase, token, method), "application/json", bytes.NewReader(body))
	if err != nil {
		telemetry.Inc("telegram_error")
		// Hata metni istek adresini, yani bot anahtarını içerebilir: yalnız yöntem yazılır.
		telemetry.Note("telegram_error", method+": ağ hatası")
		// url.Error istek adresini (bot anahtarı dahil) içerir; kayıtlara yalnız asıl hata gitsin.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("telegram %s: %w", method, urlErr.Err)
		}
		return fmt.Errorf("telegram %s: bağlantı hatası", method)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var result struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	_ = json.Unmarshal(raw, &result)
	if resp.StatusCode != http.StatusOK || !result.OK {
		description := result.Description
		if description == "" {
			description = strings.TrimSpace(string(raw))
		}
		apiErr := &APIError{Method: method, Status: resp.StatusCode, Description: description}
		if !(method == "editMessageText" && notModified(apiErr)) {
			telemetry.Inc("telegram_error")
			telemetry.Note("telegram_error", apiErr.Error())
		}
		return apiErr
	}
	telemetry.Inc("telegram_ok")
	if out != nil && len(result.Result) > 0 {
		if err := json.Unmarshal(result.Result, out); err != nil {
			return fmt.Errorf("telegram %s cevabı çözülemedi: %w", method, err)
		}
	}
	return nil
}

func (k Keyboard) markup() map[string]any {
	rows := make([][]map[string]string, 0, len(k))
	for _, row := range k {
		buttons := make([]map[string]string, 0, len(row))
		for _, b := range row {
			button := map[string]string{"text": b.Text}
			if b.URL != "" {
				button["url"] = b.URL
			} else {
				button["callback_data"] = b.Data
			}
			buttons = append(buttons, button)
		}
		rows = append(rows, buttons)
	}
	return map[string]any{"inline_keyboard": rows}
}

func richPayload(m Message) map[string]any {
	return map[string]any{"html": m.Rich, "skip_entity_detection": true}
}

func withCommon(payload map[string]any, m Message) map[string]any {
	if len(m.Keyboard) > 0 {
		payload["reply_markup"] = m.Keyboard.markup()
	}
	return payload
}

// Send mesajı gönderir: önce zengin biçim, Telegram onu reddederse sade biçim.
func Send(token, chatID string, m Message) error {
	if m.Rich != "" {
		err := call(token, "sendRichMessage", withCommon(map[string]any{
			"chat_id": chatID, "rich_message": richPayload(m), "disable_notification": m.Silent,
		}, m))
		if err == nil || !rejected(err) {
			return err
		}
		log.Printf("zengin mesaj reddedildi, sade biçim gönderiliyor: %v", err)
		telemetry.Inc("telegram_rich_fallback")
	}
	return call(token, "sendMessage", withCommon(map[string]any{
		"chat_id": chatID, "text": m.Plain, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true}, "disable_notification": m.Silent,
	}, m))
}

// Edit düğmenin bulunduğu mesajı yerinde günceller; düzenlenemezse yeni mesaj gönderir.
func Edit(token, chatID string, messageID int, m Message) error {
	if messageID == 0 {
		return Send(token, chatID, m)
	}
	if m.Rich != "" {
		err := call(token, "editMessageText", withCommon(map[string]any{
			"chat_id": chatID, "message_id": messageID, "rich_message": richPayload(m),
		}, m))
		if err == nil || notModified(err) {
			return nil
		}
		if !rejected(err) {
			return err
		}
		log.Printf("zengin düzenleme reddedildi, sade biçim deneniyor: %v", err)
		telemetry.Inc("telegram_rich_fallback")
	}
	err := call(token, "editMessageText", withCommon(map[string]any{
		"chat_id": chatID, "message_id": messageID, "text": m.Plain, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	}, m))
	if err == nil || notModified(err) {
		return nil
	}
	if !rejected(err) {
		return err
	}
	log.Printf("mesaj düzenlenemedi, yeni mesaj gönderiliyor: %v", err)
	return Send(token, chatID, m)
}

// Command Telegram'ın "/" menüsünde görünen bir komuttur.
type Command struct{ Name, Description string }

// SetCommands komut menüsünü (sohbet kutusundaki "/" ve Menü düğmesi) ayarlar.
func SetCommands(token string, commands []Command) error {
	list := make([]map[string]string, 0, len(commands))
	for _, c := range commands {
		list = append(list, map[string]string{"command": strings.TrimPrefix(c.Name, "/"), "description": c.Description})
	}
	return call(token, "setMyCommands", map[string]any{"commands": list})
}

func AnswerCallback(token, callbackQueryID, text string) {
	_ = call(token, "answerCallbackQuery", map[string]any{"callback_query_id": callbackQueryID, "text": text})
}

// Update, poller'ın bota ilettiği tek komut ya da düğme basışıdır.
type Update struct {
	Command    string
	Text       string // yazılan komutun tamamı (ör. "/uyanik 20.12"); düğmede boş
	ChatID     string
	CallbackID string
	MessageID  int // düğme basışında düğmenin bulunduğu mesaj; yazılan komutta 0
}

// skipBacklog bot kapalıyken birikmiş güncellemeleri atlar ve ilk okunacak
// offset'i döndürür. Uyku ya da deploy sırasında basılmış düğmeler (ör. saatler
// önce basılan bir ayar) bot açılınca işlenmesin diye kullanılır.
func skipBacklog(endpoint string) int {
	resp, err := telegramPollClient.Get(endpoint + "?offset=-1&timeout=0")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var last struct {
		Ok     bool `json:"ok"`
		Result []struct {
			UpdateID int `json:"update_id"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&last) != nil || !last.Ok || len(last.Result) == 0 {
		return 0
	}
	telemetry.Note("telegram_backlog_skipped", "açılışta bekleyen eski komutlar atlandı")
	log.Println("açılışta bekleyen eski Telegram komutları atlandı")
	return last.Result[len(last.Result)-1].UpdateID + 1
}

func StartPoller(token string, handler func(Update)) {
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates", apiBase, token)
	offset := skipBacklog(endpoint)
	for {
		reqURL := fmt.Sprintf("%s?offset=%d&timeout=30", endpoint, offset)
		resp, err := telegramPollClient.Get(reqURL)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var updateResp struct {
			Ok     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  *struct {
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
					Text string `json:"text"`
				} `json:"message"`
				CallbackQuery *struct {
					ID      string `json:"id"`
					Data    string `json:"data"`
					Message *struct {
						MessageID int `json:"message_id"`
						Chat      struct {
							ID int64 `json:"id"`
						} `json:"chat"`
					} `json:"message"`
				} `json:"callback_query"`
			} `json:"result"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&updateResp); err == nil && updateResp.Ok {
			for _, u := range updateResp.Result {
				offset = u.UpdateID + 1

				if u.Message != nil && isCommand(u.Message.Text) {
					handler(Update{Command: commandName(u.Message.Text), Text: u.Message.Text, ChatID: fmt.Sprintf("%d", u.Message.Chat.ID)})
				} else if u.CallbackQuery != nil && u.CallbackQuery.Message != nil {
					handler(Update{
						Command:    u.CallbackQuery.Data,
						ChatID:     fmt.Sprintf("%d", u.CallbackQuery.Message.Chat.ID),
						CallbackID: u.CallbackQuery.ID,
						MessageID:  u.CallbackQuery.Message.MessageID,
					})
				}
			}
		}
		resp.Body.Close()
	}
}

// Commands, botun tanıdığı yazılı komutlardır; SetCommands ile menüde de gösterilir.
var Commands = []Command{
	{"/menu", "Ana menü"},
	{"/notlar", "Notlarım"},
	{"/program", "Haftalık ders programı"},
	{"/sinav", "Sınav takvimi"},
	{"/ayarlar", "Takip ve uyarı ayarları"},
	{"/uyanik", "Uyanık kalma süresi (ör. /uyanik 20.12)"},
}

var aliases = map[string]bool{"/start": true, "/sinavlar": true}

// commandName "/sinav@BotAdi" gibi grup biçimini ve büyük harfi sadeleştirir.
func commandName(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	name, _, _ := strings.Cut(strings.ToLower(fields[0]), "@")
	return name
}

func isCommand(text string) bool {
	name := commandName(text)
	if aliases[name] {
		return true
	}
	for _, c := range Commands {
		if c.Name == name {
			return true
		}
	}
	return false
}

// PinnedMessage sohbette en son sabitlenen mesajın kimliğini ve düz metnini döndürür.
// Sabitlenmiş mesaj yoksa id 0'dır.
func PinnedMessage(token, chatID string) (id int, text string, err error) {
	var chat struct {
		Pinned *struct {
			MessageID int    `json:"message_id"`
			Text      string `json:"text"`
		} `json:"pinned_message"`
	}
	if err := callResult(token, "getChat", map[string]any{"chat_id": chatID}, &chat); err != nil {
		return 0, "", err
	}
	if chat.Pinned == nil {
		return 0, "", nil
	}
	return chat.Pinned.MessageID, chat.Pinned.Text, nil
}

// SendPinned biçimsiz bir metni sessizce gönderir, sabitler ve kimliğini döndürür.
func SendPinned(token, chatID, text string) (int, error) {
	var sent struct {
		MessageID int `json:"message_id"`
	}
	if err := callResult(token, "sendMessage", map[string]any{"chat_id": chatID, "text": text, "disable_notification": true}, &sent); err != nil {
		return 0, err
	}
	if err := call(token, "pinChatMessage", map[string]any{"chat_id": chatID, "message_id": sent.MessageID, "disable_notification": true}); err != nil {
		return sent.MessageID, err
	}
	return sent.MessageID, nil
}

// EditPlain biçimsiz bir mesajın metnini değiştirir; metin aynıysa hata saymaz.
func EditPlain(token, chatID string, messageID int, text string) error {
	err := call(token, "editMessageText", map[string]any{"chat_id": chatID, "message_id": messageID, "text": text})
	if notModified(err) {
		return nil
	}
	return err
}
