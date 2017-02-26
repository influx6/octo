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
	system := octo.NewBaseSystem(authenticator{}, mock.StdLogger{}, map[string]octo.MessageHandler{
		"POP": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PUSH"), nil), true)
		},
	})

	server := tcp.New(mock.StdLogger{}, tcp.ServerAttr{
		Addr:        ":8050",
		ClusterAddr: ":8060",
	})

	server.Listen(system)

	if err := server.RelateWithCluster(":6060"); err != nil {
		panic(err)
	}

	server.Wait()
}
