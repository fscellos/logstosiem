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
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var wg sync.WaitGroup // Synchronisation des goroutines en charge de la lecture sur les différentes pods du service

// const SERVICE = "cas"
// const CONTAINER_NAME = "cas"

type PodLog struct {
	Namespace     string
	PodName       string
	ContainerName string
	Follow        bool
	ClientSet     *kubernetes.Clientset
}

const SERVICE = "sadirah-courrier"
const CONTAINER_NAME = "changereactor"
const NAMESPACE = "sadirah-workflow-thd"

// Récupération des Pods associés à un service. Utiliation des labels selector pour determiner les pods cibles
func GetPodForService(ctx context.Context, namespace string, serviceName string, clientSet *kubernetes.Clientset) (error, *v1.PodList) {
	options := metav1.GetOptions{}
	service, err := clientSet.CoreV1().Services(namespace).Get(context.TODO(), serviceName, options)
	// Il faut récupérer le selector dans le service et cela permettra de rechercher les PODS correspondant avec le selector
	if err != nil {
		return err, nil
	}

	// Récupération des sélecteurs pour les utiliser pour recherche le ou les pods
	set := labels.Set(service.Spec.Selector)

	optionsPods := metav1.ListOptions{
		LabelSelector: set.AsSelector().String(),
	}
	pods, err := clientSet.CoreV1().Pods(namespace).List(context.TODO(), optionsPods)

	return nil, pods
}

// Lecture de la log des pods pour le container donné
func (podLog *PodLog) GetPodLogs(ctx context.Context, logchannel chan string) error {
	defer wg.Done()

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
	for {
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
		logchannel <- message
		// Un bloc select bloc jusqu'à ce que l'une des opérations puissent se faire
		// Tant qu'on log c'est ok pour le cas 1
		// Si on écrit dans le quitchanel, le channel de sortie est fermé et on ferme la fonction
		select {
		case <-ctx.Done():
			fmt.Print("On sort de la goroutine")
			logchannel <- "On sort de la goroutine"
			close(logchannel)
			return nil
		}
		// fmt.Print("POD " + podName + " ---- " + message + "\r\n") // Pour la suite à envoyer à une méthode en charge du parsing et de la création des messages kafka
	}
	return nil
}

// Permet de contrôler les loggers de pods
func ControlPodLogger(annulation context.CancelFunc, howmany int) error {
	for i := 0; ; i++ {
		if i == 10 {
			// On déclenche l'annulation du contexte qui doit avoir comme effet l'arrêt des goroutines
			// de captation de logs
			for j := 0; j < howmany; j++ {
				annulation()
			}
			return nil
		}
	}
	return nil
}

func main() {
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

	// Création d'un context pour permettre l'annulation des goroutines de log si la
	// goroutine de contrôle détecte une modifications dans les pods associés au service
	ctx, fn := context.WithCancel(context.TODO())

	// test pour la récupération des pods associés à un service "cas"
	err, pods := GetPodForService(ctx, NAMESPACE, SERVICE, clientset)
	if err != nil {
		panic(err.Error())
	}

	// Channel pour la récupération des logs dans la boucle principale
	logchannel := make(chan string)
	compteurToNotLog := 0
	for _, pod := range pods.Items {
		fmt.Print(pod.Name)
		podLog := &PodLog{
			Namespace:     NAMESPACE,
			PodName:       pod.Name,
			ContainerName: CONTAINER_NAME,
			Follow:        true,
			ClientSet:     clientset,
		}
		if pod.ObjectMeta.DeletionTimestamp == nil {
			wg.Add(1)
			go podLog.GetPodLogs(ctx, logchannel)
		} else {
			compteurToNotLog++
		}
	}
	// On lance la goroutine de contrôle qui va vérifier qu'il ne faut pas terminer les goroutines qui récupèrent
	//les logs sur des pods qui peuvent être terminés. Le second chiffre correspond au nombre de halt nécessaires
	go ControlPodLogger(fn, len(pods.Items)-compteurToNotLog)

	for i := range logchannel {
		fmt.Print(i)
	}

	wg.Wait()

	fmt.Print("Ok on sort du programme en ayant arrête les go routines")
}
