package order

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/enorith/ninjabot/pkg/ent"
	"github.com/enorith/ninjabot/pkg/ent/order"
	"github.com/enorith/ninjabot/pkg/exchange"
	"github.com/enorith/ninjabot/pkg/model"
	"github.com/enorith/ninjabot/pkg/notification"

	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
)

type summary struct {
	Symbol string
	Win    []float64
	Lose   []float64
}

func (s summary) Profit() float64 {
	profit := 0.0
	for _, value := range append(s.Win, s.Lose...) {
		profit += value
	}
	return profit
}

func (s summary) Payoff() float64 {
	avgWin := 0.0
	avgLose := 0.0
	for _, value := range s.Win {
		avgWin += value
	}
	for _, value := range s.Lose {
		avgLose += value
	}

	if len(s.Win) == 0 || len(s.Lose) == 0 {
		return 0
	}

	return (avgWin / float64(len(s.Win))) / math.Abs(avgLose/float64(len(s.Lose)))
}

func (s summary) String() string {
	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	_, quote := exchange.SplitAssetQuote(s.Symbol)

	data := [][]string{
		{"Coin", s.Symbol},
		{"Trades", strconv.Itoa(len(s.Lose) + len(s.Win))},
		{"Win", strconv.Itoa(len(s.Win))},
		{"Loss", strconv.Itoa(len(s.Lose))},
		{"% Win", fmt.Sprintf("%.1f", float64(len(s.Win))/float64(len(s.Win)+len(s.Lose))*100)},
		{"Payoff", fmt.Sprintf("%.1f", s.Payoff()*100)},
		{"Profit", fmt.Sprintf("%.4f %s", s.Profit(), quote)},
	}
	table.AppendBulk(data)
	table.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_RIGHT})
	table.Render()
	return tableString.String()
}

type Controller struct {
	mtx            sync.Mutex
	ctx            context.Context
	exchange       exchange.Exchange
	storage        *ent.Client
	orderFeed      *Feed
	notifier       notification.Notifier
	Results        map[string]*summary
	tickerInterval time.Duration
	finish         chan bool
}

func NewController(ctx context.Context, exchange exchange.Exchange, storage *ent.Client,
	orderFeed *Feed, notifier notification.Notifier) *Controller {

	return &Controller{
		ctx:            ctx,
		storage:        storage,
		exchange:       exchange,
		orderFeed:      orderFeed,
		notifier:       notifier,
		Results:        make(map[string]*summary),
		tickerInterval: time.Second,
		finish:         make(chan bool),
	}
}

func (c *Controller) calculateProfit(o *model.Order) (value, percent float64, err error) {
	orders, err := c.storage.Order.Query().Where(
		order.UpdatedAtLTE(o.UpdatedAt),
		order.Status(string(model.OrderStatusTypeFilled)),
		order.Symbol(o.Symbol),
		order.IDNEQ(o.ID),
	).Order(ent.Asc(order.FieldUpdatedAt)).All(c.ctx)
	if err != nil {
		return 0, 0, err
	}

	quantity := 0.0
	avgPrice := 0.0
	for _, order := range orders {
		if order.Side == string(model.SideTypeBuy) {
			price := order.Price
			if order.Type == string(model.OrderTypeStopLoss) || order.Type == string(model.OrderTypeStopLossLimit) {
				price = order.Stop
			}
			avgPrice = (order.Quantity*price + avgPrice*quantity) / (order.Quantity + quantity)
			quantity += order.Quantity
		} else {
			quantity = math.Max(quantity-order.Quantity, 0)
		}
	}

	cost := o.Quantity * avgPrice
	price := o.Price
	if o.Type == model.OrderTypeStopLoss || o.Type == model.OrderTypeStopLossLimit {
		price = *o.Stop
	}
	profitValue := o.Quantity*price - cost
	return profitValue, profitValue / cost, nil
}

func (c *Controller) notify(message string) {
	log.Info(message)
	if c.notifier != nil {
		c.notifier.Notify(message)
	}
}

func (c *Controller) processTrade(order *model.Order) {
	profitValue, profit, err := c.calculateProfit(order)
	if err != nil {
		log.Errorf("order/controller storage: %s", err)
	}
	order.Profit = profit
	if _, ok := c.Results[order.Symbol]; !ok {
		c.Results[order.Symbol] = &summary{Symbol: order.Symbol}
	}

	if profitValue >= 0 {
		c.Results[order.Symbol].Win = append(c.Results[order.Symbol].Win, profitValue)
	} else {
		c.Results[order.Symbol].Lose = append(c.Results[order.Symbol].Lose, profitValue)
	}

	_, quote := exchange.SplitAssetQuote(order.Symbol)
	c.notify(fmt.Sprintf("[PROFIT] %f %s (%f %%)\n%s", profitValue, quote, profit*100, c.Results[order.Symbol].String()))
}

func (c *Controller) Stop() {
	c.updateOrders()
	c.finish <- true
}

func (c *Controller) updateOrders() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	orders, err := c.storage.Order.Query().
		Where(order.StatusIn(
			string(model.OrderStatusTypeNew),
			string(model.OrderStatusTypePartiallyFilled),
			string(model.OrderStatusTypePendingCancel),
		)).
		Order(ent.Asc(order.FieldID)).
		All(c.ctx)
	if err != nil {
		log.Error("orderController/start:", err)
		c.mtx.Unlock()
		return
	}

	// For each pending order, check for updates
	var updatedOrders []model.Order
	for _, order := range orders {
		excOrder, err := c.exchange.Order(order.Symbol, order.ExchangeID)
		if err != nil {
			log.WithField("id", order.ExchangeID).Error("orderControler/get: ", err)
			continue
		}

		// no status change
		if string(excOrder.Status) == order.Status {
			continue
		}

		excOrder.ID = order.ID

		_, err = order.Update().
			SetUpdatedAt(excOrder.UpdatedAt).
			SetStatus(string(excOrder.Status)).
			SetQuantity(excOrder.Quantity).
			SetPrice(excOrder.Price).Save(c.ctx)
		if err != nil {
			log.Error("orderControler/update: ", err)
			continue
		}

		log.Infof("[ORDER %s] %s", excOrder.Status, excOrder)
		updatedOrders = append(updatedOrders, excOrder)
	}

	for _, processOrder := range updatedOrders {
		if processOrder.Side == model.SideTypeSell && processOrder.Status == model.OrderStatusTypeFilled {
			c.processTrade(&processOrder)
		}
		c.orderFeed.Publish(processOrder, false)
	}
}

func (c *Controller) Start() {
	go func() {
		ticker := time.NewTicker(c.tickerInterval)
		for {
			select {
			case <-ticker.C:
				c.updateOrders()
			case <-c.finish:
				ticker.Stop()
				return
			}
		}
	}()
}

func (c *Controller) createOrder(order *model.Order) error {
	register, err := c.storage.Order.Create().
		SetExchangeID(order.ExchangeID).
		SetCreatedAt(order.CreatedAt).
		SetUpdatedAt(order.UpdatedAt).
		SetPrice(order.Price).
		SetQuantity(order.Quantity).
		SetSide(string(order.Side)).
		SetSymbol(order.Symbol).
		SetType(string(order.Type)).
		SetStatus(string(order.Status)).
		SetNillableStop(order.Stop).
		SetNillableGroupID(order.GroupID).
		Save(c.ctx)
	if err != nil {
		return fmt.Errorf("error on save order: %w", err)
	}
	order.ID = register.ID
	return nil
}

func (c *Controller) Account() (model.Account, error) {
	return c.exchange.Account()
}

func (c *Controller) Position(symbol string) (asset, quote float64, err error) {
	return c.exchange.Position(symbol)
}

func (c *Controller) Order(symbol string, id int64) (model.Order, error) {
	return c.exchange.Order(symbol, id)
}

func (c *Controller) OrderOCO(side model.SideType, symbol string, size, price, stop,
	stopLimit float64) ([]model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating OCO order for %s", symbol)
	orders, err := c.exchange.OrderOCO(side, symbol, size, price, stop, stopLimit)
	if err != nil {
		log.Errorf("order/controller exchange: %s", err)
		return nil, err
	}

	for i := range orders {
		err := c.createOrder(&orders[i])
		if err != nil {
			log.Errorf("order/controller storage: %s", err)
			return nil, err
		}
		go c.orderFeed.Publish(orders[i], true)
	}

	return orders, nil
}

func (c *Controller) OrderLimit(side model.SideType, symbol string, size, limit float64) (model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating LIMIT %s order for %s", side, symbol)
	order, err := c.exchange.OrderLimit(side, symbol, size, limit)
	if err != nil {
		log.Errorf("order/controller exchange: %s", err)
		return model.Order{}, err
	}

	err = c.createOrder(&order)
	if err != nil {
		log.Errorf("order/controller storage: %s", err)
		return model.Order{}, err
	}
	go c.orderFeed.Publish(order, true)
	log.Infof("[ORDER CREATED] %s", order)
	return order, nil
}

func (c *Controller) OrderMarket(side model.SideType, symbol string, size float64) (model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating MARKET %s order for %s", side, symbol)
	order, err := c.exchange.OrderMarket(side, symbol, size)
	if err != nil {
		log.Errorf("order/controller exchange: %s", err)
		return model.Order{}, err
	}

	err = c.createOrder(&order)
	if err != nil {
		log.Errorf("order/controller storage: %s", err)
		return model.Order{}, err
	}

	// calculate profit
	if order.Side == model.SideTypeSell {
		c.processTrade(&order)
	}

	go c.orderFeed.Publish(order, true)
	log.Infof("[ORDER CREATED] %s", order)
	return order, err
}

func (c *Controller) Cancel(order model.Order) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Cancelling order for %s", order.Symbol)
	err := c.exchange.Cancel(order)
	if err != nil {
		return err
	}

	_, err = c.storage.Order.UpdateOneID(order.ID).
		SetStatus(string(model.OrderStatusTypePendingCancel)).
		Save(c.ctx)
	if err != nil {
		log.Errorf("order/controller storage: %s", err)
		return err
	}
	log.Infof("[ORDER CANCELED] %s", order)
	return nil
}
