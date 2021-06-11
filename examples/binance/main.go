package main

import (
	"context"
	"log"
	"os"

	"github.com/enorith/ninjabot"
	"github.com/enorith/ninjabot/examples/strategies"
	"github.com/enorith/ninjabot/pkg/exchange"
	"github.com/enorith/ninjabot/pkg/model"
	"github.com/enorith/ninjabot/pkg/notification"
)

func main() {
	var (
		ctx             = context.Background()
		apiKey          = os.Getenv("API_KEY")
		secretKey       = os.Getenv("API_SECRET")
		telegramKey     = os.Getenv("TELEGRAM_KEY")
		telegramID      = os.Getenv("TELEGRAM_ID")
		telegramChannel = os.Getenv("TELEGRAM_CHANNEL")
	)

	settings := model.Settings{
		Pairs: []string{
			"BTCUSDT",
			"ETHUSDT",
		},
	}

	// Initialize your exchange
	binance, err := exchange.NewBinance(ctx, exchange.WithBinanceCredentials(apiKey, secretKey))
	if err != nil {
		log.Fatalln(err)
	}

	// (Optional) Telegram notifier
	notifier := notification.NewTelegram(telegramID, telegramKey, telegramChannel)

	strategy := &strategies.CrossEMA{}
	bot, err := ninjabot.NewBot(ctx, settings, binance, strategy)
	if err != nil {
		log.Fatalln(err)
	}

	bot.SubscribeOrder(notifier)

	err = bot.Run(ctx)
	if err != nil {
		log.Fatalln(err)
	}
}
