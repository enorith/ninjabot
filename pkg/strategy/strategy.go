package strategy

import (
	"github.com/enorith/ninjabot/pkg/exchange"
	"github.com/enorith/ninjabot/pkg/model"
)

type Strategy interface {
	Init(settings model.Settings)
	Timeframe() string
	WarmupPeriod() int
	Indicators(dataframe *model.Dataframe)
	OnCandle(dataframe *model.Dataframe, broker exchange.Broker)
}
