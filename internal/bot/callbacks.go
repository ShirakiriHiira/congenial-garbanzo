package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	callbackPrefix = "v1:"
	callbackView   = "view"
	callbackRenew  = "renew"
)

// handleCallback treats the callback payload as untrusted input. It answers
// every query exactly once, including unauthorized, stale, and malformed ones.
func (h *Handler) handleCallback(ctx context.Context, b *telegram.Bot, cq *models.CallbackQuery) {
	if cq == nil {
		return
	}
	answer := "操作失败"
	defer func() {
		if _, err := b.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{CallbackQueryID: cq.ID, Text: answer}); err != nil {
			h.logger.Warn("telegram callback answer failed", "code", "telegram_error")
		}
	}()

	action, jobID, ok := parseCallback(cq.Data)
	if !ok {
		answer = "未知操作"
		return
	}
	if cq.Message.Message == nil {
		answer = "请在私聊中使用"
		return
	}
	msg := cq.Message.Message
	if msg.Chat.Type != models.ChatTypePrivate || cq.From.ID <= 0 || msg.Chat.ID != cq.From.ID {
		answer = "请在私聊中使用"
		return
	}
	uid := cq.From.ID
	if !h.svc.Allowed(uid) {
		answer = "无权限使用此机器人"
		return
	}
	user, err := h.svc.Authenticate(ctx, uid, &msg.Chat.ID)
	if err != nil {
		answer = serviceMessage(err)
		return
	}
	period, err := h.svc.PeriodForJob(ctx, user.ID, jobID)
	if err != nil {
		if service.Code(err) == "conflict" {
			answer = "该提醒所属周期已更新"
		} else {
			answer = serviceMessage(err)
		}
		return
	}
	job, err := h.svc.GetJob(ctx, user.ID, jobID)
	if err != nil {
		answer = serviceMessage(err)
		return
	}
	switch action {
	case callbackView:
		answer = "详情已发送"
		h.send(ctx, b, msg.Chat.ID, fmt.Sprintf("提醒 #%s 状态 %s（已尝试 %d 次）\n\n%s", id(job.ID), job.Status, job.Attempts, periodDetail(period)))
	case callbackRenew:
		dialog, err := h.svc.StartRenewDialog(ctx, user.ID, jobID, msg.Chat.ID)
		if err != nil {
			if service.Code(err) == "conflict" {
				answer = "该提醒所属周期已更新"
			} else {
				answer = serviceMessage(err)
			}
			return
		}
		answer = "请输入新的截止日期"
		h.send(ctx, b, msg.Chat.ID, fmt.Sprintf("续期有效期 #%s（当前周期 %d）\n请直接回复，例如：\ndate_precision=date&expires_at=2027-01-31\n也可附加 &amount=1000&currency=TWD&notes=...\n15 分钟内有效；发送 /cancel 可取消。\n\n%s", id(dialog.ValidityPeriodID), dialog.ExpectedCycle, periodDetail(period)))
	}
}

// parseCallback accepts only v1:view|renew:<decimal job id>.
func parseCallback(data string) (action string, jobID int64, ok bool) {
	if !strings.HasPrefix(data, callbackPrefix) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(data, callbackPrefix)
	parts := strings.Split(rest, ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	switch parts[0] {
	case callbackView, callbackRenew:
	default:
		return "", 0, false
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || n < 1 {
		return "", 0, false
	}
	return parts[0], n, true
}
