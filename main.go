package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

const tokenEnv = "TELEGRAM_BOT_TOKEN"

func main() {
	token := loadToken()
	if token == "" {
		log.Fatalf("%s is not set: set it in .env or in the environment", tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	b, err := bot.New(token, bot.WithDefaultHandler(defaultHandler))
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)

	log.Println("bot started, press Ctrl+C to stop")
	b.Start(ctx)
	log.Println("bot stopped")
}

// loadToken reads the token from .env first and falls back to the process environment.
func loadToken() string {
	if values, err := godotenv.Read(".env"); err == nil {
		if token := values[tokenEnv]; token != "" {
			return token
		}
	}
	return os.Getenv(tokenEnv)
}

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	reply(ctx, b, update.Message, "Hi! I am your reminder bot. Send me a text and I will echo it back.")
}

func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	if update.Message.Text == "" {
		reply(ctx, b, update.Message, "I can only handle text messages for now.")
		return
	}
	reply(ctx, b, update.Message, update.Message.Text)
}

func reply(ctx context.Context, b *bot.Bot, msg *models.Message, text string) {
	if msg == nil {
		return
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   text,
	}); err != nil {
		log.Printf("send message to chat %d: %v", msg.Chat.ID, err)
	}
}
