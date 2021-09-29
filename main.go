/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Note: the example only works with the code within the same release/branch.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var wg sync.WaitGroup // Synchronisation des goroutines en charge de la lecture sur les différentes pods du service
var compileRegex *regexp.Regexp
var kafkaConn *kafka.Conn
var SERVICE = "test"
var CONTAINER_NAME = "test"
var NAMESPACE = "test"
var WATCH_PERIOD int64 = 10 // Periode de vérification des changements
const REGEXP = "^(WHO|WHAT|ACTION|APPLICATION|WHEN|CLIENT IP ADDRESS|SERVER IP ADDRESS): (.*)"

// Kafka configuration
const KAFKA_BROKER_ADRESSE = "10.37.180.52:9092"
const KAFKA_TOPIC = "testtopic"

type PodLog struct {
	Namespace     string
	PodName       string
	ContainerName string
	Follow        bool
	ClientSet     *kubernetes.Clientset
}

type PodLogControl struct {
	StopCollecteur chan struct{}
	PodNames       map[string]struct{}
	ClientSet      *kubernetes.Clientset
}

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

// Send back true if all fields of SiemMessage is complete
func (sm *SiemMessage) isComplete() bool {
	complete := false
	complete = sm.ACTION != "" && sm.APPLICATION != "" && sm.CLIENTIP != "" && sm.SERVERIP != "" && sm.WHAT != "" && sm.WHEN != "" && sm.WHO != ""
	return complete
}

// Récupération des Pods associés à un service. Utiliation des labels selector pour determiner les pods cibles
// Méthode OK
func GetPodForService(ctx context.Context, namespace string, serviceName string, clientSet *kubernetes.Clientset) (error, *v1.PodList) {
	options := metav1.GetOptions{}
	service, err := clientSet.CoreV1().Services(namespace).Get(ctx, serviceName, options)
	// Il faut récupérer le selector dans le service et cela permettra de rechercher les PODS correspondant avec le selector
	if err != nil {
		return err, nil
	}

	// Récupération des sélecteurs pour les utiliser pour recherche le ou les pods
	set := labels.Set(service.Spec.Selector)

	optionsPods := metav1.ListOptions{
		LabelSelector: set.AsSelector().String(),
	}
	pods, err := clientSet.CoreV1().Pods(namespace).List(ctx, optionsPods)

	return nil, pods
}

// Lecture de la log des pods pour le container donné
func (podLog *PodLog) GetPodLogs(ctx context.Context, quitChannel chan struct{}) error {
	count := int64(100)
	podLogOptions := v1.PodLogOptions{
		Container: podLog.ContainerName,
		Follow:    podLog.Follow,
		TailLines: &count,
	}
	podLogRequest := podLog.ClientSet.CoreV1().
		Pods(podLog.Namespace).
		GetLogs(podLog.PodName, &podLogOptions)
	stream, err := podLogRequest.Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close() // Le stream et fermé quand la méthode termine son exécution
	var siemMessage = SiemMessage{
		PodName: podLog.PodName,
	}
	for {
		select {
		case <-quitChannel: // Channel de terminaison Si il est clos on decrease la synchronisation et on quitte la goroutine
			fmt.Println("Lecture du pod " + podLog.PodName + " il faut qu'on quitte")
			wg.Done() // Permettra la libération de la routine principale permettant de relancer une consulation
			return nil
		default: // Sinon on continue la lecture des logs
			buf := make([]byte, 2000)
			numBytes, err := stream.Read(buf)
			if numBytes == 0 {
				continue
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			message := string(buf[:numBytes])
			if len(message) > 0 {
				siemMessage, _ = parseAndSend(message, siemMessage) // Et on traite les logs en question pour les transformer et les envoyer ailleurs (ie. kafka)
			}
		}
	}
}

// Permet de contrôler les loggers de pods. Lancer un traitement toutes les minutes
// Pour checker que les pods sont bien en vie. Sinon relance la recherche et collecte
func (plc *PodLogControl) ControlPodLogger() error {
	// On lance un ticker pour l'exécution péridique
	eachMinutes := time.NewTicker(time.Duration(WATCH_PERIOD) * time.Second)
	for {
		select {
		case <-eachMinutes.C: // Channel en lecture seul, appelé tous les WATCH_PERIOD secondes
			if cancel, _ := Processing(plc); cancel { // En cas de signal de fin, on arrête le timer et on stop la goroutine
				fmt.Println("On arrete le timer")
				eachMinutes.Stop()
				return nil
			}
		}
	}
}

// Traitement de vérification/relance éventuelle
func Processing(plc *PodLogControl) (bool, error) {
	err, pods := GetPodForService(context.TODO(), NAMESPACE, SERVICE, plc.ClientSet)
	if err != nil {
		panic(err.Error())
	}

	// Si l'un des pods a disparu on annule tout et on recommence
	cancellation := false
	for _, pod := range pods.Items {
		if pod.ObjectMeta.DeletionTimestamp == nil { // Uniquement si le pod est actif
			if _, found := plc.PodNames[pod.Name]; !found {
				cancellation = true // Un pod diffère on relance la collecte sur l'ensemble
			}
		}
	}

	if cancellation {
		fmt.Println("Il va falloir annuler car nous ne sommes plus sur les bons pods")
		// On ferme le channel "quit" pour déclencher la sortie de toutes les goroutines et relancer une recherche
		// de pod dans la boucle principale
		fmt.Println("On envoi le signal d'annulation")
		close(plc.StopCollecteur) // Clos le channel de terminaison qui va arrêter les routines de collecte, la routine de contrôle et relancer
		// une recherche de collecte depuis la fonction main
		return true, nil
	}
	return false, nil
}

// Boucle de traitement qui va être réappelée à chaque fois que l'un des pods est modifié
// Pour adapter le nombre ou le paramétrage en charge du suivi des logs pour chaque pods
func processCurrentPods(clientset *kubernetes.Clientset, quitChannel chan struct{}) {
	// Création d'un context simple car on passera par un channel fermé pour annuler la lecture des logs
	ctx := context.TODO()

	// Récupération des pods associés à un service
	err, pods := GetPodForService(ctx, NAMESPACE, SERVICE, clientset)
	if err != nil {
		panic(err.Error())
	}

	podsName := make(map[string]struct{})
	for _, pod := range pods.Items {
		podLog := &PodLog{
			Namespace:     NAMESPACE,
			PodName:       pod.Name,
			ContainerName: CONTAINER_NAME,
			Follow:        true,
			ClientSet:     clientset,
		}
		// Si le Pod est terminating on ne log pas dessus
		if pod.ObjectMeta.DeletionTimestamp == nil {
			podsName[pod.Name] = struct{}{}
			fmt.Println("Lancement collecte log pour " + pod.Name)
			wg.Add(1)                              // On suit les différents collecteurs de log (dans le main permet de redéclencher une recherche si chgt d'éat)
			go podLog.GetPodLogs(ctx, quitChannel) // On lance la goroutine qui va suivre le pod
		}
	}
	// On lance la goroutine de contrôle qui va vérifier qu'il ne faut pas terminer les goroutines qui récupèrent
	//les logs sur des pods qui peuvent être terminés.
	podLogControl := &PodLogControl{
		StopCollecteur: quitChannel, // Channel d'interruption si chgt dans les pods suivis
		PodNames:       podsName,    // Les pods qu'on suit actuellement pour comparaison avec l'état du système
		ClientSet:      clientset,   // Le client k8s
	}
	fmt.Println("Lancement goroutine de controle")
	go podLogControl.ControlPodLogger()

	// Boucle bloquante tant que le channel de terminaison n'est pas clos(ie. personne n'écrit dessus)
	for {
		select {
		case <-quitChannel:
			fmt.Println("On quitte la boucle de contrôle")
			return
		}
	}
}

func main() {
	// RESTE A INTEGRER UN SYSTEME DE LOG SIMPLE, LA CONFIGURATION ET KUSTOMIZE

	// Configuration kafka
	kafkaConfiguration()

	// Compilation expression régulière de reherche
	var compError error
	compileRegex, compError = regexp.Compile(REGEXP)
	if compError != nil {
		fmt.Println("Problème lors de la compilation de l'expression régulière")
		panic(compError)
	}

	// creates the in-cluster config
	// config, err := rest.InClusterConfig()
	// if err != nil {
	// 	panic(err.Error())
	// }

	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// On boucle sur la création d'un channel de terminaison (qui nous bloquera dans la boucle)
	// Et on lance le traitement de recherches des pods et de collecte des logs
	for {
		// On créé le channel qui sera fermé en cas de changement et qui permettra de relancer
		// le traitement de recherche des pods et a collecte des logs
		quitChannel := make(chan struct{})

		// On appelle traitement de collecte
		processCurrentPods(clientset, quitChannel)

		select {
		case <-quitChannel: // On lit le channel de terminaison. Il est bloquant tant qu'il n'est pas fermé.
			fmt.Println("Réception d'un signal de terminaison car l'état des pods a bougé")
			wg.Wait() // POint de synchronisation ouvert lorsque les pods de lecture seront tous terminés
			fmt.Println("On relance donc de zero le processing de traitement des nouveaux pods")
		}
	}
}

func parseAndSend(message string, existingSiemMessage SiemMessage) (SiemMessage, error) {

	// On splitte les retours chariots
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		matches := compileRegex.FindStringSubmatch(line)
		if matches != nil {
			switch matches[1] {
			case "WHO":
				existingSiemMessage.WHO = matches[2]
			case "WHAT":
				existingSiemMessage.WHAT = matches[2]
			case "ACTION":
				existingSiemMessage.ACTION = matches[2]
			case "APPLICATION":
				existingSiemMessage.APPLICATION = matches[2]
			case "WHEN":
				existingSiemMessage.WHEN = matches[2]
			case "CLIENT IP ADDRESS":
				existingSiemMessage.CLIENTIP = matches[2]
			case "SERVER IP ADDRESS":
				existingSiemMessage.SERVERIP = matches[2]
			}
			if existingSiemMessage.isComplete() { // Si le message est complet on l'envoit
				sendToKafka(existingSiemMessage)
				existingSiemMessage = SiemMessage{
					PodName: existingSiemMessage.PodName,
				} // Et on le réinitialise pour la prochaine log
			}
		}
	}

	return existingSiemMessage, nil // A alimenter correctement avec un vrai SiemMessage
}

func sendToKafka(siemMessage SiemMessage) error {
	marshalsiemfordebug, _ := json.Marshal(siemMessage)

	_, err := kafkaConn.WriteMessages(kafka.Message{
		Value: marshalsiemfordebug,
	})
	if err != nil {
		fmt.Printf("Erreur pendant l'envoi du message", err.Error())
		return err
	} else {
		fmt.Println("OK écriture du message dans kafka")
	}

	return nil
}

// Initialisation du client kafka
func kafkaConfiguration() error {

	conn, err := kafka.DialLeader(context.TODO(), "tcp", KAFKA_BROKER_ADRESSE, KAFKA_TOPIC, 0)
	if err != nil {
		fmt.Printf("Erreur lors de la connexion au kafka " + err.Error())
		panic(err)
	}
	kafkaConn = conn

	return nil
}
