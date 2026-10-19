package bot

import (
	"context"
	"strconv"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// v3 callback payloads carry an action and decimal IDs only. Drafts, notes,
// phone numbers, ICCIDs and URLs never enter callback data. Old v2 payloads are
// rejected without reading or writing any business state.
const callbackPrefixV3 = "v3:"

func cb(parts ...string) string {
	return callbackPrefixV3 + strings.Join(parts, ":")
}

func callbackID(parts []string, index int) (int64, bool) {
	if index < 0 || index >= len(parts) {
		return 0, false
	}
	n, err := strconv.ParseInt(parts[index], 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

type callbackV3 struct {
	scope  string
	action string
	args   []string
}

// parseCallbackV3 splits v3:scope:action[:arg...] without validating the
// action allowlist; handlers answer unknown scopes/actions exactly once.
func parseCallbackV3(data string) (callbackV3, bool) {
	if !strings.HasPrefix(data, callbackPrefixV3) {
		return callbackV3{}, false
	}
	parts := strings.Split(strings.TrimPrefix(data, callbackPrefixV3), ":")
	if len(parts) < 2 || len(parts) > 7 {
		return callbackV3{}, false
	}
	for _, part := range parts {
		if part == "" {
			return callbackV3{}, false
		}
	}
	return callbackV3{scope: parts[0], action: parts[1], args: parts[2:]}, true
}

func button(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func markup(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	filtered := make([][]models.InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			filtered = append(filtered, row)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: filtered}
}

func pair(buttons ...models.InlineKeyboardButton) []models.InlineKeyboardButton {
	return buttons
}

// sendMarkup sends the text (splitting oversized bodies) with the keyboard
// attached to the final chunk so Telegram always renders it with the body.
func (h *Handler) sendMarkup(ctx context.Context, b *telegram.Bot, chatID int64, text string, keyboard *models.InlineKeyboardMarkup) {
	if text == "" && keyboard == nil {
		return
	}
	parts := chunks(text, 3800)
	if len(parts) == 0 {
		parts = []string{""}
	}
	for i, chunk := range parts {
		params := &telegram.SendMessageParams{ChatID: chatID, Text: chunk}
		if i == len(parts)-1 && keyboard != nil {
			params.ReplyMarkup = *keyboard
		}
		if chunk == "" {
			params.Text = "请选择："
		}
		if _, err := b.SendMessage(ctx, params); err != nil {
			h.logger.Warn("telegram reply failed", "code", "telegram_error")
			return
		}
	}
}
