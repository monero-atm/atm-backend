package main

import (
	"encoding/json"
	"encoding/base64"
	"net/http"
	"fmt"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/rs/zerolog/log"
	"github.com/gorilla/websocket"
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

	// OpenKiosk events
	okUpdate chan proto.Event

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
	okUpdate = make(chan proto.Event)

	session = &sessionData{
		broker: connectToBroker(),
	}

	go session.appLogic()
	go pricePoll(cfg.CurrencyShort, cfg.FiatEurRate)
	go mpayHealthPoll()

	http.HandleFunc("/ws", atmSessionHandler)
	log.Fatal().Err(http.ListenAndServe(cfg.Bind, nil)).Msg("Failed to bind")
}

func atmSessionHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Websocket update")
		return
	}
	session.conn = c
	cmd(session.broker, "codescannerd", "start")
	go session.handleIncoming()
	go session.handleOutgoing()
}

// Updates arriving from the frontend: cancel transaction, stop price update
func (s *sessionData) handleIncoming() {
	for {
		mt, message, err := s.conn.ReadMessage()
		if err != nil {
			log.Error().Err(err).Msg("Websocket read")
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
				log.Error().Err(err).Msg("Websocket write")
				continue
			}
		}
	}
}

// Main application logic happens here.
// Make sense of all updates.
func (s *sessionData) appLogic() {
	for {
		select {
		case frontendUpdate := <- incoming:
			var front update
			if err := json.Unmarshal(frontendUpdate, front); err != nil {
				log.Error().Err(err).Msg("Malformed frontend update")
				continue
			}
			switch (front.Event) {
			case "start":
				s.state = AddressIn
				// Pause price updates and health checks
				pricePause <- true
				mpayHealthPause <- true
				log.Info().Msg("Began new transaction")
			case "cancel":
				// Reset all data from previous transaction
				s.state = Idle
				s.address = ""
				s.fiats = nil
				s.xmr = 0
				s.fee = 0
				s.err = nil
				s.tx = nil

				// Enable price updates
				pricePause <- false
				mpayHealthPause <- false

				// Stop bill acceptor, enable QR code scanning
				cmd(s.broker, "moneyacceptord", "stop")
				cmd(s.broker, "codescannerd", "start")

				log.Info().Msg("Cancelled transaction")
			}
		case hardwareUpdate := <- okUpdate:
			log.Info().Str("type", hardwareUpdate.Event).Msg("")
			if hardwareUpdate.Event == "codescan" {
				log.Info().Str("data", fmt.Sprintf("%v", hardwareUpdate)).Msg("")
				data, err := proto.GetScanData(hardwareUpdate.Data)
				if err != nil {
					log.Error().Err(err).Msg("Failed to unmarshall scan data")
					continue
				}
				decoded, err := base64.StdEncoding.DecodeString(data.Scan)
				if err != nil {
					log.Error().Err(err).Msg("Failed to base64 decode scan data")
					continue
				}
				addr := parseAddress(string(decoded))
				if err := addressValidator(addr); err != nil {
					log.Error().Err(err).Msg("Invalid address received")
					if err := sendToFrontend(update{Event:"error", Data: err.Error()}); err != nil {
						log.Error().Err(err).Msg("Failed to send to frontend")
					}
				}
				s.address = addr
				if err := sendToFrontend(update{Event:"addressin", Data: addr}); err != nil {
					log.Error().Err(err).Msg("Failed to send to frontend")
				}

				// If this transaction began not by tapping the screen but by scanning QR
				if s.state == Idle {
					pricePause <- true
					mpayHealthPause <- true
					log.Info().Msg("Began new transaction")
					s.state = MoneyIn
				}
				s.state = MoneyIn
				cmd(s.broker, "codescannerd", "stop")
				cmd(s.broker, "moneyacceptord", "start")
			}
			if hardwareUpdate.Event == "moneyin" {
				log.Info().Str("data", fmt.Sprintf("%v", hardwareUpdate)).Msg("")
			}
		}
	}
}
