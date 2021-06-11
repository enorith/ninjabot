package main

import (
	"context"

	"github.com/enorith/ninjabot"
	"github.com/enorith/ninjabot/examples/strategies"
	"github.com/enorith/ninjabot/pkg/exchange"
	"github.com/enorith/ninjabot/pkg/model"
	"github.com/enorith/ninjabot/pkg/plot"
	"github.com/enorith/ninjabot/pkg/storage"

	log "github.com/sirupsen/logrus"
)

func main() {
	ctx := context.Background()

	settings := model.Settings{
		Pairs: []string{
			"BTCUSDT",
			"ETHUSDT",
		},
	}

	strategy := new(strategies.CrossEMA)

	csvFeed, err := exchange.NewCSVFeed(
		strategy.Timeframe(),
		exchange.PairFeed{
			Pair:      "BTCUSDT",
			File:      "testdata/btc-1h.csv",
			Timeframe: "1h",
		},
		exchange.PairFeed{
			Pair:      "ETHUSDT",
			File:      "testdata/eth-1h.csv",
			Timeframe: "1h",
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	storage, err := storage.FromMemory()
	if err != nil {
		log.Fatal(err)
	}

	wallet := exchange.NewPaperWallet(
		ctx,
		"USDT",
		exchange.WithPaperAsset("USDT", 10000),
		exchange.WithDataFeed(csvFeed),
	)

	chart := plot.NewChart()

	bot, err := ninjabot.NewBot(
		ctx,
		settings,
		wallet,
		strategy,
		ninjabot.WithStorage(storage),
		ninjabot.WithCandleSubscription(wallet),
		ninjabot.WithCandleSubscription(chart),
		ninjabot.WithOrderSubscription(chart),
		ninjabot.WithLogLevel(log.WarnLevel),
	)
	if err != nil {
		log.Fatal(err)
	}

	err = bot.Run(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Print bot results
	bot.Summary()
	wallet.Summary()
	err = chart.Start()
	if err != nil {
		log.Fatal(err)
	}
}
