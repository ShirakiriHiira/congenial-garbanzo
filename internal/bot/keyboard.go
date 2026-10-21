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

// responder routes one response sequence for an update. The first emission
// replaces the callback message when editID is set; later emissions always send
// new messages so a single update can never rewrite more than one message.
type responder struct {
	chatID int64
	editID int
	used   bool
}

// next reserves the target for the current emission.
func (r *responder) next() (chatID int64, editID int) {
	if r.used {
		return r.chatID, 0
	}
	r.used = true
	return r.chatID, r.editID
}

// editNotModified reports the one Telegram 400 that means the message already
// shows the requested content.
func editNotModified(err error) bool {
	return err != nil && strings.Contains(err.Error(), "message is not modified")
}

// noKeyboard removes the buttons of an edited message. Telegram treats an
// omitted reply_markup as "keep the current keyboard", so clearing is explicit.
var noKeyboard = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}

// sendMarkup sends the text (splitting oversized bodies) with the keyboard
// attached to the final chunk so Telegram always renders it with the body. When
// the responder still owes the callback message and the text fits in one chunk,
// the message is rewritten instead of adding another bubble.
func (h *Handler) sendMarkup(ctx context.Context, b *telegram.Bot, resp *responder, text string, keyboard *models.InlineKeyboardMarkup) {
	if text == "" && keyboard == nil {
		return
	}
	parts := chunks(text, 3800)
	if len(parts) == 0 {
		parts = []string{""}
	}
	chatID, editID := resp.next()
	if editID > 0 && len(parts) == 1 {
		body := parts[0]
		if body == "" {
			body = "请选择："
		}
		replyMarkup := models.ReplyMarkup(noKeyboard)
		if keyboard != nil {
			replyMarkup = keyboard
		}
		if _, err := b.EditMessageText(ctx, &telegram.EditMessageTextParams{ChatID: chatID, MessageID: editID, Text: body, ReplyMarkup: replyMarkup}); err == nil || editNotModified(err) {
			return
		}
		h.logger.Warn("telegram edit failed", "code", "telegram_error")
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
