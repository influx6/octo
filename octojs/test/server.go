package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	classicHttp "net/http"
	"os"
	"os/signal"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/streams/server"
	"github.com/influx6/octo/streams/server/http"
	"github.com/influx6/octo/streams/server/websocket"
)

var (
	pocket = octo.AuthCredential{
		Scheme: "XScheme",
		Key:    "Rack",
		Token:  "4343121-GU",
		Data:   "Teddybear",
	}
)

type mockSystem struct{}

// Authenticate authenticates the provided credentials and implements
// the octo.Authenticator interface.
func (mockSystem) Authenticate(cred octo.AuthCredential) error {
	if cred.Scheme != pocket.Scheme {
		return errors.New("Scheme does not match")
	}

	if cred.Key != pocket.Key {
		return errors.New("Key does not match")
	}

	if cred.Token != pocket.Token {
		return errors.New("Token does not match")
	}

	return nil
}

// Serve handles the processing of different requests coming from the outside.
func (mockSystem) Serve(message []byte, tx server.Stream) error {
	fmt.Printf("Message: %+q\n", message)

	cmds, err := jsoni.Parser.Decode(message)
	if err != nil {
		return err
	}

	commands, ok := cmds.([]jsoni.CommandMessage)
	if !ok {
		return consts.ErrParseError
	}

	var res bytes.Buffer

	for _, command := range commands {
		switch command.Name {
		case "PUMP":
			res.WriteString("DUMP\r\n")
			continue
		case "REX":
			res.WriteString("DEX\r\n")
		default:
			return errors.New("Invalid Command")
		}
	}

	return tx.Send(res.Bytes(), true)
}

func main() {
	var system mockSystem

	instruments := instruments.Instruments(mock.NewLogger(), nil)

	httpServer := http.New(instruments, http.BasicAttr{
		Addr:         "127.0.0.1:5060",
		Authenticate: true,
		Auth:         pocket,
	})

	socketServer := websocket.New(instruments, websocket.SocketAttr{
		Authenticate: true,
		Addr:         "127.0.0.1:6060",
		OriginValidator: func(r *classicHttp.Request) bool {
			return true
		},
	})

	if err := httpServer.Listen(system); err != nil {
		log.Fatalf("Failed to start http server: %+q", err)
	}

	if err := socketServer.Listen(system); err != nil {
		log.Fatalf("Failed to start http server: %+q", err)
	}

	defer socketServer.Close()
	defer httpServer.Close()

	log.Printf("HTTP Server started @ %+q", httpServer.Attr.Addr)
	log.Printf("Websocket Server started @ %+q", socketServer.Attr.Addr)

	// Listen for an interrupt signal from the OS.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan
}
