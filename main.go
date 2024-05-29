package main

import (
	"github.com/eclipse/paho.golang/autopaho"
	mpay "gitlab.com/moneropay/moneropay/v2/pkg/model"
	"gitlab.com/openkiosk/proto"
)

type State int

const (
	Idle State = iota
	AddressIn
	MoneyIn
	TxInfo
)

type model struct {
	broker     *autopaho.ConnectionManager
	state      State
	address    string
	fiat       uint64
	xmr        uint64
	fee        float64
	xmrPrice   float64
	err        error
	height     int
	width      int
	mpayHealth bool
	tx         *mpay.TransferPostResponse
}

var sub chan proto.Event

var priceUpdate chan priceEvent
var pricePause chan bool

var mpayHealthUpdate chan mpayHealthEvent
var mpayHealthPause chan bool

func main() {
	cfg = loadConfig()
	priceUpdate = make(chan priceEvent)
	pricePause = make(chan bool)
	mpayHealthUpdate = make(chan mpayHealthEvent)
	mpayHealthPause = make(chan bool)

	go pricePoll(cfg.CurrencyShort, cfg.FiatEurRate)
	go mpayHealthPoll()
}
