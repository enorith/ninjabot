package strategies

import (
	"github.com/enorith/ninjabot/pkg/exchange"
	"github.com/enorith/ninjabot/pkg/model"

	"github.com/markcheno/go-talib"
	log "github.com/sirupsen/logrus"
)

type OCOSell struct{}

func (e OCOSell) Init(settings model.Settings) {}

func (e OCOSell) Timeframe() string {
	return "1d"
}

func (e OCOSell) WarmupPeriod() int {
	return 9
}

func (e OCOSell) Indicators(df *model.Dataframe) {
	df.Metadata["stoch"], df.Metadata["stoch_signal"] = talib.Stoch(
		df.High,
		df.Low,
		df.Close,
		8,
		3,
		talib.SMA,
		3,
		talib.SMA,
	)
}

func (e *OCOSell) OnCandle(df *model.Dataframe, broker exchange.Broker) {
	closePrice := df.Close.Last(0)
	log.Info("New Candle = ", df.Pair, df.LastUpdate, closePrice)

	assetPosition, quotePosition, err := broker.Position(df.Pair)
	if err != nil {
		log.Error(err)
	}

	buyAmount := 4000.0
	if quotePosition > buyAmount && df.Metadata["stoch"].Crossover(df.Metadata["stoch_signal"]) {
		size := buyAmount / closePrice
		_, err := broker.OrderMarket(model.SideTypeBuy, df.Pair, size)
		if err != nil {
			log.WithFields(map[string]interface{}{
				"pair":  df.Pair,
				"side":  model.SideTypeBuy,
				"close": closePrice,
				"asset": assetPosition,
				"quote": quotePosition,
				"size":  size,
			}).Error(err)
		}

		_, err = broker.OrderOCO(model.SideTypeSell, df.Pair, size, closePrice*1.05, closePrice*0.95, closePrice*0.95)
		if err != nil {
			log.WithFields(map[string]interface{}{
				"pair":  df.Pair,
				"side":  model.SideTypeBuy,
				"close": closePrice,
				"asset": assetPosition,
				"quote": quotePosition,
				"size":  size,
			}).Error(err)
		}
	}
}
