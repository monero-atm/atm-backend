package main

import (
	"log"
	"net/http"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/gorilla/websocket"
	mpay "gitlab.com/moneropay/moneropay/v2/pkg/model"
	//"gitlab.com/openkiosk/proto"
)

type State int

const (
	Idle State = iota
	AddressIn
	MoneyIn
	TxInfo
)

type update struct {
	// keyword description of what happened
	Event string      `json:"event"`
	Data  interface{} `json:"value"`
}

type fiat struct {
	amount   uint64
	currency string
}

type sessionData struct {
	conn       *websocket.Conn
	broker     *autopaho.ConnectionManager
	state      State
	address    string
	fiats      []fiat
	xmr        uint64
	fee        float64
	xmrPrice   float64
	err        error
	height     int
	width      int
	mpayHealth bool
	tx         *mpay.TransferPostResponse
}

var (
	session *sessionData

	// Updates from frontend
	incoming chan []byte

	// Updates to frontend
	outgoing chan []byte

	priceUpdate chan priceEvent
	pricePause  chan bool

	mpayHealthUpdate chan mpayHealthEvent
	mpayHealthPause  chan bool
)

var upgrader = websocket.Upgrader{} // use default options

func main() {
	cfg = loadConfig()
	incoming = make(chan []byte)
	outgoing = make(chan []byte)
	priceUpdate = make(chan priceEvent)
	pricePause = make(chan bool)
	mpayHealthUpdate = make(chan mpayHealthEvent)
	mpayHealthPause = make(chan bool)

	session = &sessionData{
		broker: connectToBroker(),
	}

	go pricePoll(cfg.CurrencyShort, cfg.FiatEurRate)
	go mpayHealthPoll()

	http.HandleFunc("/ws", atmSessionHandler)
	log.Fatal(http.ListenAndServe(cfg.Bind, nil))
}

func atmSessionHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	session.conn = c
	go session.handleIncoming()
	go session.handleOutgoing()
}

// Updates arriving from the frontend: cancel transaction, stop price update
func (s *sessionData) handleIncoming() {
	for {
		mt, message, err := s.conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			continue
		}

		// Skip non-text messages
		if mt != 1 {
			continue
		}
		incoming <- message
	}
}

// Updates to the frontend: money inserted, sent, backend error
func (s *sessionData) handleOutgoing() {
	for {
		select {
		case m := <-outgoing:
			err := s.conn.WriteMessage(2, m)
			if err != nil {
				log.Println("write:", err)
				continue
			}
		}
	}
}
