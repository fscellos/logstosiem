package connectors

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	"github.com/segmentio/kafka-go"
)

// Message formaté pour le système Siem
type SiemMessage struct {
	PodName     string `json:"podname"`
	WHO         string `json:"who"`
	WHAT        string `json:"what"`
	ACTION      string `json:"action"`
	APPLICATION string `json:"application"`
	WHEN        string `json:"when"`
	CLIENTIP    string `json:"clientip"`
	SERVERIP    string `json:"serverip"`
}

// Retourne vrai si tous les champs du  SiemMessage sont renseignés
func (sm *SiemMessage) IsComplete() bool {
	complete := false
	complete = sm.ACTION != "" && sm.APPLICATION != "" && sm.CLIENTIP != "" && sm.SERVERIP != "" && sm.WHAT != "" && sm.WHEN != "" && sm.WHO != ""
	return complete
}

// Kafka configuration : ces éléments seront à configurer par la suite
const KAFKA_BROKER_ADRESSE = "10.37.180.52:9092"
const KAFKA_TOPIC = "testtopic"

// On reprend ici la définition de l'interface. Pour faire plus propre on devrait la poser dans son propre fichier
type MiddleWare interface {
	Send(siemMessage SiemMessage) error
	Configure(ctx context.Context) error
}

// Notre structure qui implémenterar l'interface Middleware
type KafkaMiddleWare struct {
	KafkaConn *kafka.Conn
	Logger    logr.Logger
}

// Première méthode respectant l'interface MiddleWare pour la fonction "send"
func (k KafkaMiddleWare) Send(siemMessage SiemMessage) error {
	marshalsiemfordebug, _ := json.Marshal(siemMessage)
	_, err := k.KafkaConn.WriteMessages(kafka.Message{
		Value: marshalsiemfordebug,
	})
	if err != nil {
		k.Logger.V(4).Error(err, "Erreur pendant l'envoi du message", "message", siemMessage)
		return err
	} else {
		k.Logger.V(4).Info("Ok message écrit dans kafka", "message", siemMessage)
	}
	return nil
}

// Seconde méthode respectant l'interface MiddleWare pour la fonction "send"
func (k KafkaMiddleWare) Configure(ctx context.Context) error {
	conn, err := kafka.DialLeader(context.TODO(), "tcp", KAFKA_BROKER_ADRESSE, KAFKA_TOPIC, 0)
	if err != nil {
		k.Logger.Error(err, "Erreur lors de la connexion au kafka")
		return err
	}
	k.KafkaConn = conn
	return nil
}
