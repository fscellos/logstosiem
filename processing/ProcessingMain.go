package processing

import (
	"context"
	"flag"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"com.sfr/labgo/connectors"
	"github.com/go-logr/glogr"
	"github.com/go-logr/logr"
	core "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const REGEXP = "^(WHO|WHAT|ACTION|APPLICATION|WHEN|CLIENT IP ADDRESS|SERVER IP ADDRESS): (.*)"

type ProcessingMain struct {
	Logger        logr.Logger
	CompileRegex  *regexp.Regexp
	MessageSender connectors.MiddleWare
	WG            sync.WaitGroup // Synchronisation des goroutines en charge de la lecture sur les différentes pods du service
	K8sCLient     *kubernetes.Clientset
	Context       context.Context
}

func (main ProcessingMain) InitLog() {
	// Initialisation des logs :
	//  - Niveau v 1 (va de 0 jusqu'à 5) qui sera le niveau par défaut (utilisable avec les options -v en ligne de commande par exemple)
	//  - On
	_ = flag.Set("logtostderr", "true")
	flag.Parse()

	main.Logger = glogr.NewWithOptions(glogr.Options{}).WithName("Main")
	// main.Logger.Info("Visible par défaut")              // Cette log apparait toujours
	// main.Logger.V(2).Info("Log non visible par défaut") // Cette log n'apparaitra que si on spécifie le paramètre -v à l'exécution
}

// Initialisation de la configuration Kubernetes
func (main ProcessingMain) InitKubernetes() error {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		main.Logger.V(4).Error(err, "Erreur lors de la récupération de la configuration in-cluster")
		config, err = main.initK8sExternal()
		if err != nil {
			main.Logger.V(4).Error(err, "Erreur lors de la récupération de la configuration out-cluster")
			return err
		}
	}

	// creates the clientset
	main.K8sCLient, err = kubernetes.NewForConfig(config)
	if err != nil {
		main.Logger.V(4).Error(err, "Erreur lors de l'initialisation du client k8s")
		return err
	}
	return nil
}

// Cas initialisation interne
func (main ProcessingMain) initK8sExternal() (*rest.Config, error) {
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
		main.Logger.V(4).Error(err, "Erreur lors de la récupération de la configuration out-cluster")
		return nil, err
	}

	return config, nil
}

func (main ProcessingMain) ParseAndSend(message string, existingSiemMessage connectors.SiemMessage) (connectors.SiemMessage, error) {
	if main.CompileRegex == nil {
		var compError error
		main.CompileRegex, compError = regexp.Compile(REGEXP)
		if compError != nil {
			main.Logger.V(4).Error(compError, "Problème lors de la compilation de l'expression régulière %s", REGEXP)
			return existingSiemMessage, compError
		}
	}
	var newExistingSiemMessage connectors.SiemMessage
	// On splitte les retours chariots
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		matches := main.CompileRegex.FindStringSubmatch(line)
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
			if existingSiemMessage.IsComplete() { // Si le message est complet on l'envoie
				main.MessageSender.Send(existingSiemMessage)
				newExistingSiemMessage = connectors.SiemMessage{
					PodName: existingSiemMessage.PodName,
				} // Et on le réinitialise pour la prochaine log
			}
		}
	}

	return newExistingSiemMessage, nil // A alimenter correctement avec un vrai SiemMessage
}

// Boucle de traitement principale
func (main ProcessingMain) MainProcessingLoop(namespace string, service string, quitChannel chan struct{}) error {
	// Récupération des pods associés à un service
	pods, err := main.GetPodsForService(namespace, service)
	if err != nil {
		main.Logger.V(4).Error(err, "Erreur lors de la récupération des pods", "servicename", service)
		return err
	}

	// Lancement des collectes de log
	for _, pod := range pods.Items {
		// Si le Pod est terminating on ne log pas dessus
		if pod.ObjectMeta.DeletionTimestamp == nil {
			main.Logger.V(4).Info("Lancement collecte de log pour ", "pod", pod.Name)
			main.WG.Add(1)                                                              // On suit les différents collecteurs de log (dans le main permet de redéclencher une recherche si chgt d'éat)
			go main.RetrievePodLogs(namespace, pod.Name, "CONTAINER_NAME", quitChannel) // On lance la goroutine qui va suivre le pod
		}
	}

	main.Logger.V(4).Info("Lancement de la routine de contrôle")
	go main.ControlPodsChange(namespace, service, pods, quitChannel)

	// Boucle bloquante tant que le channel de terminaison n'est pas clos(ie. personne n'écrit dessus)
	for {
		select {
		case <-quitChannel:
			main.Logger.V(4).Info("Modification détectée, on quitte la structure principale")
			return nil
		}
	}
}

// Fonction de lecture des logs
func (main ProcessingMain) RetrievePodLogs(namespace string, podName string, containerName string, quitChannel chan struct{}) error {
	// On commence par construire la requête qui va permettre de stream les logs
	// Tout d'abord les options; on indique en particulier le nombre de ligne qu'on va récupérer
	count := int64(100)
	podLogOptions := v1.PodLogOptions{
		Container: containerName,
		Follow:    true,
		TailLines: &count,
	}

	podLogRequest := main.K8sCLient.CoreV1().Pods(namespace).GetLogs(podName, &podLogOptions)

	stream, err := podLogRequest.Stream(main.Context)
	if err != nil {
		main.Logger.V(4).Error(err, "Erreur lors de la création du stream de lecture pour le pod", "podname", podName)
		return err
	}
	defer stream.Close() // Le stream est fermé quand la méthode termine son exécution

	// Initialisation d'un message de base
	var siemMessage = connectors.SiemMessage{
		PodName: podName,
	}

	for {
		select {
		case <-quitChannel: // Channel de terminaison Si il est clos on decrease la synchronisation et on quitte la goroutine
			main.Logger.V(4).Info("Signal de fin de lecture sur le pod", "pod", podName)
			main.WG.Done() // Permettra la libération de la routine principale permettant de relancer une consulation
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
				siemMessage, _ = main.ParseAndSend(message, siemMessage) // Et on traite les logs en question pour les transformer et les envoyer ailleurs (ie. kafka)
			}
		}
	}

	return nil
}

// Récupération des pods du service en entrée
func (main ProcessingMain) GetPodsForService(namespace string, serviceName string) (*core.PodList, error) {
	newContext, cancelFunction := context.WithTimeout(main.Context, time.Duration(time.Duration.Milliseconds(500)))

	defer cancelFunction()
	var options metav1.GetOptions = metav1.GetOptions{}
	service, error := main.K8sCLient.CoreV1().Services(namespace).Get(newContext, serviceName, options)
	if error != nil {
		main.Logger.V(4).Error(error, "Erreur lors de la récupération du service", "ServiceName", serviceName)
		return nil, error
	}

	// Récupération des labels selector du service pour pouvoir rechercher les pods associés
	selector := labels.Set(service.Spec.Selector)

	// Utilisation de l'option pour la recherche des pods
	optionsPods := metav1.ListOptions{
		LabelSelector: selector.AsSelector().String(),
	}

	// Et recherche effective des Pods
	pods, error := main.K8sCLient.CoreV1().Pods(namespace).List(main.Context, optionsPods)
	if error != nil {
		main.Logger.V(4).Error(error, "Erreur lors de la récupération de la liste des pods pour le service", "ServiceName", serviceName)
		return nil, error
	}
	return pods, nil
}

var WATCH_PERIOD int64 = 10 // Periode de vérification des changements
// Fonction lancée comme Go routine qui va se charger de vérifier que
func (main ProcessingMain) ControlPodsChange(namespace string, serviceName string, pods *core.PodList, quitChannel chan struct{}) error {
	periodic := time.NewTicker(time.Duration(WATCH_PERIOD) * time.Second)
	for {
		select {
		case <-periodic.C: // Channel en lecture seul, appelé tous les WATCH_PERIOD secondes
			if cancel, _ := main.ProcessingChange(namespace, serviceName, pods); cancel { // En cas de signal de fin, on arrête le timer et on stop la goroutine
				main.Logger.V(4).Info("Modification détectée. On arrête le timer")
				periodic.Stop()
				close(quitChannel)
				return nil
			}
		}
	}
}

// Recherche d'un écart dans les pods entre avant et aprè
func (main ProcessingMain) ProcessingChange(namespace string, serviceName string, pods *core.PodList) (bool, error) {
	newpods, err := main.GetPodsForService(namespace, serviceName)
	if err != nil {
		main.Logger.V(4).Info("Erreur lors de la recherche des pods", "servicename", serviceName)
		return false, err // On n'arrive pas à se décider donc par défaut on continue à logguer
	}

	// Construciton d'une map avec les noms des pods en clé pour simplifier la recherche
	podsName := make(map[string]struct{})
	for _, pod := range pods.Items {
		podsName[pod.Name] = struct{}{}
	}

	// Si l'un des pods a disparu on annule tout et on recommence
	cancellation := false
	for _, pod := range newpods.Items {
		if pod.ObjectMeta.DeletionTimestamp == nil { // Uniquement si le pod est actif
			if _, found := podsName[pod.Name]; !found {
				cancellation = true // Un pod diffère on relance la collecte sur l'ensemble
			}
		}
	}

	return cancellation, nil
}
