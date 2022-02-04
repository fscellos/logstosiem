package main

import (
	"context"

	"com.sfr/labgo/connectors"
	"com.sfr/labgo/processing"
	"com.sfr/labgo/util"
	openapiclient "gitlab.pic.services.prod/valentine.io/manage/n7s/harborapi.git"
)

const NAMESPACE = "namespace"
const SERVICE_NAME = "service"

func main() {
	processingMain := processing.ProcessingMain{}
	processingMain.InitLog()

	projectName := "sadirah"
	configuration := openapiclient.NewConfiguration()
	configuration.HTTPClient = util.RootCAManager.HttpClient
	configuration.Servers[0].URL = "https://harbor.nauarkhos.dev/api"

	api_client := openapiclient.NewAPIClient(configuration)

	resp, _, err := api_client.RepositoryApi.ListRepositories(context.Background(), projectName).Execute()
	if err != nil {
		processingMain.Logger.Error(err, "Problement lors de l'appel de l'API")
	}

	for _, repo := range resp {
		processingMain.Logger.Info("Ok reponse bien arrivee " + *repo.Name)
	}

	processingMain.InitKubernetes()
	processingMain.Context = context.TODO()

	// Initialisation du client Kafka
	middleware := connectors.KafkaMiddleWare{
		Logger: processingMain.Logger,
	}

	processingMain.MessageSender = middleware

	middleware.Configure(context.TODO())

	// On boucle sur la création d'un channel de terminaison (qui nous bloquera dans la boucle)
	// Et on lance le traitement de recherches des pods et de collecte des logs
	for {
		// On créé le channel qui sera fermé en cas de changement et qui permettra de relancer
		// le traitement de recherche des pods et a collecte des logs
		quitChannel := make(chan struct{})

		// On appelle traitement de collecte
		processingMain.MainProcessingLoop(NAMESPACE, SERVICE_NAME, quitChannel)

		select {
		case <-quitChannel: // On lit le channel de terminaison. Il est bloquant tant qu'il n'est pas fermé.
			processingMain.Logger.V(4).Info("Réception d'un signal de terminaison car l'état des pods a bougé")
			processingMain.WG.Wait() // POint de synchronisation ouvert lorsque les pods de lecture seront tous terminés
			processingMain.Logger.V(4).Info("On relance donc de zero le processing de traitement des nouveaux pods")
		}
	}
}
