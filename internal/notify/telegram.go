package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func SendTelegram(token, chatID, message string) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	resp, err := http.PostForm(endpoint, url.Values{
		"chat_id":    {chatID},
		"text":       {message},
		"parse_mode": {"Markdown"},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func SendMenu(token, chatID, message string, isPaused, isDersSecmeActive bool) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	
	// Dinamik butonlar
	pauseText := "⏸ Taramayı Durdur"
	pauseCmd := "cmd_pause"
	if isPaused {
		pauseText = "▶️ Taramayı Başlat"
		pauseCmd = "cmd_resume"
	}

	dersSecmeText := "📋 Ders Seçme Takibi Aç"
	dersSecmeCmd := "cmd_ders_secme_on"
	if isDersSecmeActive {
		dersSecmeText = "🚫 Ders Seçme Takibi Kapat"
		dersSecmeCmd = "cmd_ders_secme_off"
	}

	kb := fmt.Sprintf(`{"inline_keyboard": [
		[{"text": "📖 Anlık Notlar", "callback_data": "cmd_grades"}, {"text": "📊 Sistem Durumu", "callback_data": "cmd_stats"}],
		[{"text": "%s", "callback_data": "%s"}],
		[{"text": "%s", "callback_data": "%s"}],
		[{"text": "🔄 Botu Yeniden Başlat", "callback_data": "cmd_restart"}]
	]}`, dersSecmeText, dersSecmeCmd, pauseText, pauseCmd)

	resp, err := http.PostForm(endpoint, url.Values{
		"chat_id":      {chatID},
		"text":         {message},
		"parse_mode":   {"Markdown"},
		"reply_markup": {kb},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram menu hata: %d", resp.StatusCode)
	}
	return nil
}

func AnswerCallback(token, callbackQueryID, text string) {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", token)
	http.PostForm(endpoint, url.Values{
		"callback_query_id": {callbackQueryID},
		"text":              {text},
	})
}

func StartPoller(token string, handler func(command, chatID, callbackQueryID string)) {
	var offset int
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", token)
	for {
		reqURL := fmt.Sprintf("%s?offset=%d&timeout=30", endpoint, offset)
		resp, err := http.Get(reqURL)
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
						Chat struct {
							ID int64 `json:"id"`
						} `json:"chat"`
					} `json:"message"`
				} `json:"callback_query"`
			} `json:"result"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&updateResp); err == nil && updateResp.Ok {
			for _, u := range updateResp.Result {
				offset = u.UpdateID + 1

				if u.Message != nil && u.Message.Text == "/start" {
					handler("/start", fmt.Sprintf("%d", u.Message.Chat.ID), "")
				} else if u.CallbackQuery != nil && u.CallbackQuery.Message != nil {
					handler(u.CallbackQuery.Data, fmt.Sprintf("%d", u.CallbackQuery.Message.Chat.ID), u.CallbackQuery.ID)
				}
			}
		}
		resp.Body.Close()
	}
}
