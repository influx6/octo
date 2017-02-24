package main

import (
	"github.com/influx6/faux/utils"
	"github.com/influx6/octo"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/transmission/tcp"
)

type authenticator struct{}

func (authenticator) Authenticate(auth octo.AuthCredential) error {
	return nil
}

func main() {
	system := octo.NewBaseSystem(authenticator{}, map[string]octo.MessageHandler{
		"CLOSE": func(m utils.Message, tx octo.Transmission) error {
			defer tx.Close()

			return tx.Send(utils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"POP": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PUSH"), nil), true)
		},
	})

	server := tcp.NewServer(mock.StdLogger{}, tcp.ServerAttr{
		Addr:        ":6050",
		ClusterAddr: ":6060",
	})

	server.Listen(system)
	server.Wait()
}
