// Package bot implements the Telegram command and callback interface.
//
// Business state lives in internal/service; this package only validates transport
// concerns, renders plain text, and persists the minimal renewal dialog. There is
// no echo/fallback path: unrecognized updates are ignored or answered with usage.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Handler processes Telegram updates for one process. Work is bounded so a
// shutdown cannot wait forever on a stuck request.
type Handler struct {
	svc        *service.Service
	logger     *slog.Logger
	username   atomic.Value // string; the bot's @name without the leading @
	stopped    atomic.Bool
	registered atomic.Bool
}

// New builds the handler. username may be empty; Register discovers it via getMe
// when available. A nil logger falls back to slog.Default.
func New(svc *service.Service, username string, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{svc: svc, logger: logger}
	h.SetUsername(username)
	return h
}

// SetUsername records the bot username used to filter /command@otherbot.
func (h *Handler) SetUsername(name string) {
	h.username.Store(strings.TrimPrefix(strings.TrimSpace(name), "@"))
}

// Username returns the configured bot username without the leading @.
func (h *Handler) Username() string {
	v, _ := h.username.Load().(string)
	return v
}

// Stop prevents new updates from being accepted. In-flight handlers continue
// with their own bounded context.
func (h *Handler) Stop() { h.stopped.Store(true) }

// Register installs a single catch-all handler. It replaces the SDK's logging
// default handler so updates never get echoed or dumped. If the username is not
// known yet, getMe is attempted once and a failure is logged without sensitive
// details.
func (h *Handler) Register(b *telegram.Bot) {
	if b == nil {
		return
	}
	if h.Username() == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		me, err := b.GetMe(ctx)
		cancel()
		if err != nil || me == nil {
			h.logger.Warn("telegram identity unavailable", "code", "telegram_error")
		} else {
			h.SetUsername(me.Username)
		}
	}
	if h.registered.CompareAndSwap(false, true) {
		b.RegisterHandlerMatchFunc(func(*models.Update) bool { return true }, h.Handle)
	}
}

// Handle is the SDK entry point. Handlers run synchronously under the production
// worker count; cancellation of the update loop does not interrupt an accepted
// business transaction mid-flight.
func (h *Handler) Handle(ctx context.Context, b *telegram.Bot, update *models.Update) {
	if update == nil {
		return
	}
	if h.stopped.Load() {
		if update.CallbackQuery != nil {
			// A callback must still be acknowledged; otherwise Telegram keeps the
			// client spinner alive and may redeliver the update on restart.
			ack, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			if _, err := b.AnswerCallbackQuery(ack, &telegram.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID, Text: "服务正在重启，请稍后重试"}); err != nil {
				h.logger.Warn("telegram callback answer failed", "code", "telegram_error")
			}
		}
		return
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	switch {
	case update.Message != nil:
		h.handleMessage(work, b, update.Message)
	case update.CallbackQuery != nil:
		h.handleCallback(work, b, update.CallbackQuery)
	}
}

func (h *Handler) handleMessage(ctx context.Context, b *telegram.Bot, msg *models.Message) {
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if msg.Chat.Type != models.ChatTypePrivate || msg.Chat.ID != msg.From.ID {
		// Never expose card data in groups, channels, or mismatched private chats.
		if strings.HasPrefix(text, "/") {
			target := msg.Chat.ID
			if msg.Chat.Type == models.ChatTypePrivate {
				target = msg.From.ID // never trust a sender-relative chat mismatch
			}
			h.send(ctx, b, target, "请在私聊中使用此机器人。")
		}
		return
	}
	uid := msg.From.ID
	if !h.svc.Allowed(uid) {
		h.send(ctx, b, msg.Chat.ID, "无权限使用此机器人。")
		return
	}
	user, err := h.svc.Authenticate(ctx, uid, &msg.Chat.ID)
	if err != nil {
		h.send(ctx, b, msg.Chat.ID, serviceMessage(err))
		return
	}
	if text == "" {
		h.send(ctx, b, msg.Chat.ID, "请发送文本命令，使用 /help 查看用法。")
		return
	}
	if !strings.HasPrefix(text, "/") {
		h.handleDialogInput(ctx, b, user, msg, text)
		return
	}
	h.handleCommand(ctx, b, user, msg, text)
}

func (h *Handler) handleCommand(ctx context.Context, b *telegram.Bot, user service.User, msg *models.Message, text string) {
	name, target, rest, ok := splitCommand(text)
	if !ok {
		h.send(ctx, b, msg.Chat.ID, "未知命令。\n\n"+helpText(user.Timezone))
		return
	}
	if target != "" {
		known := h.Username()
		if known == "" || !strings.EqualFold(target, known) {
			return
		}
	}

	type command func(context.Context, service.User, string) (string, error)
	var run command
	business := true
	switch name {
	case "start", "help":
		business = false
		run = func(_ context.Context, u service.User, _ string) (string, error) {
			return helpText(u.Timezone), nil
		}
	case "add":
		run = h.commandAdd
	case "list":
		run = h.commandList
	case "view":
		run = h.commandView
	case "edit":
		run = h.commandEdit
	case "delete":
		run = h.commandDelete
	case "reminders":
		run = h.commandReminders
	case "renew":
		run = h.commandRenew
	case "cancel":
		run = h.commandCancel
		if rest == "" {
			business = false // bare /cancel is the dialog cancellation command itself
		}
	default:
		h.send(ctx, b, msg.Chat.ID, "未知命令。\n\n"+helpText(user.Timezone))
		return
	}

	notice := ""
	if business {
		var err error
		notice, err = h.clearDialog(ctx, user.ID)
		if err != nil {
			h.send(ctx, b, msg.Chat.ID, serviceMessage(err))
			return
		}
	}
	out, err := run(ctx, user, rest)
	if err != nil {
		var ue userError
		if errors.As(err, &ue) {
			out = ue.msg
		} else {
			out = serviceMessage(err)
		}
	}
	h.send(ctx, b, msg.Chat.ID, notice+out)
}

// clearDialog removes a pending renewal dialog when a different business command
// starts. It reports whether a dialog was actually removed.
func (h *Handler) clearDialog(ctx context.Context, uid int64) (string, error) {
	live := true
	if _, err := h.svc.GetDialog(ctx, uid); err != nil {
		if service.Code(err) != "not_found" {
			return "", err
		}
		live = false // an expired row may still exist and is purged below
	}
	removed, err := h.svc.CancelDialog(ctx, uid)
	if err != nil {
		return "", err
	}
	if live && removed {
		return "已取消未完成的续期对话。\n", nil
	}
	return "", nil
}

func (h *Handler) handleDialogInput(ctx context.Context, b *telegram.Bot, user service.User, msg *models.Message, text string) {
	d, err := h.svc.GetDialog(ctx, user.ID)
	if err != nil {
		if service.Code(err) == "not_found" {
			h.send(ctx, b, msg.Chat.ID, "当前没有进行中的续期对话。\n\n"+helpText(user.Timezone))
			return
		}
		h.send(ctx, b, msg.Chat.ID, serviceMessage(err))
		return
	}
	if d.ChatID != msg.Chat.ID {
		h.send(ctx, b, msg.Chat.ID, "当前续期对话已在其他会话中开始；请先发送 /cancel 取消。")
		return
	}
	in, err := parseRenewInput(text, false)
	if err != nil {
		h.send(ctx, b, msg.Chat.ID, "续期失败："+err.Error()+"\n对话仍保留，可重新发送或发送 /cancel 取消。")
		return
	}
	renewal, err := h.svc.RenewFromDialog(ctx, user.ID, in)
	if err != nil {
		h.send(ctx, b, msg.Chat.ID, "续期失败："+serviceMessage(err)+"\n对话仍保留，可重新发送或发送 /cancel 取消。")
		return
	}
	h.send(ctx, b, msg.Chat.ID, renewalText(ctx, h.svc, user, renewal, "续期完成"))
}

// splitCommand parses "/name@bot rest". It returns the lowercase command name,
// the target bot name (possibly empty), and the remaining arguments.
func splitCommand(text string) (name, target, rest string, ok bool) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '/' {
		return "", "", "", false
	}
	head := text[1:]
	if i := strings.IndexAny(head, " \t\r\n"); i >= 0 {
		rest = strings.TrimSpace(head[i:])
		head = head[:i]
	}
	if head == "" {
		return "", "", "", false
	}
	if i := strings.IndexByte(head, '@'); i >= 0 {
		target = head[i+1:]
		head = head[:i]
		if target == "" {
			return "", "", "", false
		}
	}
	name = strings.ToLower(head)
	for _, r := range name {
		if r < 'a' || r > 'z' {
			return "", "", "", false
		}
	}
	if name == "" {
		return "", "", "", false
	}
	return name, target, rest, true
}

func (h *Handler) send(ctx context.Context, b *telegram.Bot, chatID int64, text string) {
	if text == "" {
		return
	}
	for _, chunk := range chunks(text, 3800) {
		if _, err := b.SendMessage(ctx, &telegram.SendMessageParams{ChatID: chatID, Text: chunk}); err != nil {
			h.logger.Warn("telegram reply failed", "code", "telegram_error")
			return
		}
	}
}

func chunks(text string, size int) []string {
	if size <= 0 || utf8.RuneCountInString(text) <= size {
		return []string{text}
	}
	var out []string
	runes := []rune(text)
	for len(runes) > size {
		out = append(out, string(runes[:size]))
		runes = runes[size:]
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

func fail(msg string) error { return userError{msg: msg} }

func serviceMessage(err error) string {
	switch service.Code(err) {
	case "invalid_input":
		return "输入无效，请检查格式和数值。"
	case "not_found":
		return "对象不存在。"
	case "conflict":
		return "操作冲突：数据已更新或当前状态不允许此操作，请重新查看后重试。"
	case "forbidden":
		return "无权限使用此机器人。"
	default:
		return "服务暂时不可用，请稍后重试。"
	}
}
