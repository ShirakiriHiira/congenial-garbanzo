package bot

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	"telegram.bot.remind/internal/reminder"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Sender is the production reminder delivery adapter. It owns only the Telegram
// boundary: the scheduler supplies the durable payload and this type classifies
// failures without leaking SDK URLs or response bodies.
type Sender struct {
	bot    *telegram.Bot
	logger *slog.Logger
}

var _ reminder.Sender = (*Sender)(nil)

// NewSender wires the delivery adapter to the shared bot instance.
func NewSender(b *telegram.Bot, logger *slog.Logger) *Sender {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sender{bot: b, logger: logger}
}

// SendReminder sends one reminder with the two phase-one buttons and returns the
// Telegram message ID. The error is always a *reminder.DeliveryError.
func (s *Sender) SendReminder(ctx context.Context, chatID int64, text string, jobID int64) (int64, error) {
	if s.bot == nil {
		return 0, &reminder.DeliveryError{Code: "unknown"}
	}
	value := strconv.FormatInt(jobID, 10)
	msg, err := s.bot.SendMessage(ctx, &telegram.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "已续期", CallbackData: "v1:renew:" + value},
			{Text: "查看详情", CallbackData: "v1:view:" + value},
		}}},
	})
	if err != nil {
		failure := classify(err)
		s.logger.Warn("reminder delivery failed", "code", failure.Code)
		return 0, failure
	}
	if msg == nil {
		return 0, &reminder.DeliveryError{Code: "unknown"}
	}
	return int64(msg.ID), nil
}

// classify maps SDK errors to the scheduler's fixed vocabulary. The SDK exposes
// sentinel errors for known HTTP statuses and typed errors for 429/migration, so
// no response text is parsed or retained.
func classify(err error) *reminder.DeliveryError {
	var tooMany *telegram.TooManyRequestsError
	if errors.As(err, &tooMany) {
		retry := time.Duration(tooMany.RetryAfter) * time.Second
		if retry < 0 {
			retry = 0
		}
		return &reminder.DeliveryError{Code: "rate_limited", RetryAfter: retry}
	}
	var migrate *telegram.MigrateError
	if errors.As(err, &migrate) {
		return &reminder.DeliveryError{Code: "remote_rejected", Permanent: true}
	}
	for _, permanent := range []error{
		telegram.ErrorBadRequest, telegram.ErrorUnauthorized, telegram.ErrorForbidden,
		telegram.ErrorNotFound, telegram.ErrorConflict,
	} {
		if errors.Is(err, permanent) {
			return &reminder.DeliveryError{Code: "remote_rejected", Permanent: true}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &reminder.DeliveryError{Code: "network"}
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return &reminder.DeliveryError{Code: "network"}
	}
	var netErr *net.OpError
	var urlErr *url.Error
	if errors.As(err, &netErr) || errors.As(err, &urlErr) {
		return &reminder.DeliveryError{Code: "network"}
	}
	// Unclassified API failures (typically 5xx or malformed responses) are
	// retryable but bounded by the scheduler's attempt limit.
	return &reminder.DeliveryError{Code: "remote_unavailable"}
}
