package test

import (
	"context"
	"testing"

	"com.sfr/labgo/connectors"
	"com.sfr/labgo/processing"
)

func TestParseAndSend(t *testing.T) {
	racine := processing.ProcessingMain{}
	racine.InitLog()

	sm := connectors.SiemMessage{
		PodName:     "Toto",
		WHO:         "Quelqu'un",
		WHAT:        "Quelque chose",
		ACTION:      "Quelque chose aussi",
		APPLICATION: "Test",
		WHEN:        "Hier",
		CLIENTIP:    "0.0.0.0",
	}
	fm := FakeMiddleWare{}

	racine.MessageSender = fm

	sm2, _ := racine.ParseAndSend("quelque chose ici \net encore\nSERVER IP ADDRESS: 255.254.253.252\nautre", sm)

	if retour.SERVERIP != "255.254.253.252" {
		t.Errorf("Oups l'IP serveur est incorrect. Attendu %s et obtenu %s", "255.254.253.252", retour.SERVERIP)
	}

	if retour.PodName != sm2.PodName || sm2.ACTION != "" || sm2.APPLICATION != "" {
		t.Errorf("Oups le message de retour de la fonction ne semble pas en rapport avec le précédent: %s vs %s", retour.PodName, sm2.PodName)
	}

}

var retour connectors.SiemMessage

type FakeMiddleWare struct {
	SiemMessage connectors.SiemMessage
}

func (fm FakeMiddleWare) Send(siemMessage connectors.SiemMessage) error {
	fm.SiemMessage = siemMessage
	retour = siemMessage

	return nil
}

func (fm FakeMiddleWare) Configure(ctx context.Context) error {
	return nil
}
