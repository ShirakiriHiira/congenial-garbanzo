package bot

import (
	"context"
	"strconv"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// v2 callback payloads carry an action and decimal IDs only. Drafts, notes,
// phone numbers, ICCIDs, and URLs never enter callback data.
const callbackPrefixV2 = "v2:"

func cb(parts ...string) string {
	return callbackPrefixV2 + strings.Join(parts, ":")
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

type callbackV2 struct {
	scope  string
	action string
	args   []string
}

// parseCallbackV2 splits v2:scope:action[:arg...] without validating the
// action allowlist; handlers answer unknown scopes/actions exactly once.
func parseCallbackV2(data string) (callbackV2, bool) {
	if !strings.HasPrefix(data, callbackPrefixV2) {
		return callbackV2{}, false
	}
	parts := strings.Split(strings.TrimPrefix(data, callbackPrefixV2), ":")
	if len(parts) < 2 || len(parts) > 6 {
		return callbackV2{}, false
	}
	for _, part := range parts {
		if part == "" {
			return callbackV2{}, false
		}
	}
	return callbackV2{scope: parts[0], action: parts[1], args: parts[2:]}, true
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
