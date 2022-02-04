package util

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"io/ioutil"
	"net/http"
	"strings"

	configuration "com.sfr/labgo/config"
)

var RootCAManager AdditionalRootCAManager

// Structure
type AdditionalRootCAManager struct {
	HttpClient *http.Client
}

// Initialisation du client http
func init() {
	// Si pas de certficats additionel, on sort
	if strings.EqualFold(configuration.NOTSET, configuration.Configuration.AdditionalTrustedCA) {
		return
	}

	// On récupère le fichier
	files, _ := ioutil.ReadDir(configuration.Configuration.AdditionalTrustedCA)

	// Permet de supporter une option en entrée autorisant le mode insecure
	insecure := flag.Bool("insecure-ssl", false, "Accept/Ignore all server SSL certificates")
	flag.Parse()
	rootCAs, _ := x509.SystemCertPool() // Le Pool de CA système
	if rootCAs == nil {
		rootCAs = x509.NewCertPool() // Qu'on initialise si non défini
	}

	// On lit le certificat et on l'ajoute à notre pool de Root CA
	for _, localCertFile := range files {
		certs, _ := ioutil.ReadFile(configuration.Configuration.AdditionalTrustedCA + "/" + localCertFile.Name())
		_ = rootCAs.AppendCertsFromPEM(certs)
	}

	// Création des options TLS du client http
	tlsconfig := &tls.Config{
		InsecureSkipVerify: *insecure,
		RootCAs:            rootCAs,
	}
	tr := &http.Transport{TLSClientConfig: tlsconfig}
	RootCAManager = AdditionalRootCAManager{}
	RootCAManager.HttpClient = &http.Client{Transport: tr}
}
