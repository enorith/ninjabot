package main

import (
	"context"
	"os"

	"github.com/enorith/ninjabot"
	"github.com/enorith/ninjabot/examples/strategies"
	"github.com/enorith/ninjabot/pkg/exchange"
	"github.com/enorith/ninjabot/pkg/model"
	"github.com/enorith/ninjabot/pkg/notification"
	"github.com/enorith/ninjabot/pkg/storage"

	log "github.com/sirupsen/logrus"
)

func main() {
	var (
		ctx             = context.Background()
		telegramKey     = os.Getenv("TELEGRAM_KEY")
		telegramID      = os.Getenv("TELEGRAM_ID")
		telegramChannel = os.Getenv("TELEGRAM_CHANNEL")
	)

	settings := model.Settings{
		Pairs: []string{
			"BTCUSDT",
			"ETHUSDT",
			"BNBUSDT",
			"LTCUSDT",
		},
	}

	// Use binance for realtime data feed
	binance, err := exchange.NewBinance(ctx)
	if err != nil {
		log.Fatal(err)
	}

	storage, err := storage.FromFile("backtest.db")
	if err != nil {
		log.Fatal(err)
	}

	notifier := notification.NewTelegram(telegramID, telegramKey, telegramChannel)
	paperWallet := exchange.NewPaperWallet(
		ctx,
		"USDT",
		exchange.WithPaperFee(0.001, 0.001),
		exchange.WithPaperAsset("USDT", 10000),
		exchange.WithDataFeed(binance),
	)

	strategy := new(strategies.CrossEMA)
	bot, err := ninjabot.NewBot(
		ctx,
		settings,
		paperWallet,
		strategy,
		ninjabot.WithStorage(storage),
		ninjabot.WithNotifier(notifier),
		ninjabot.WithCandleSubscription(paperWallet),
	)
	if err != nil {
		log.Fatalln(err)
	}

	err = bot.Run(ctx)
	if err != nil {
		log.Fatalln(err)
	}
}
