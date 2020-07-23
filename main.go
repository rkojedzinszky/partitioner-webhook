package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/namsral/flag"
)

func main() {
	port := flag.Int("port", 8443, "Port to listen on")
	cert := flag.String("tls-cert-file", "/tls/tls.crt", "TLS Certificate")
	key := flag.String("tls-key-file", "/tls/tls.key", "TLS private key")
	kubeConfig := flag.String("kube-config", "", "Kubernetes configuration. If empty, will use in-cluster configuration")

	flag.Parse()

	certificate, err := tls.LoadX509KeyPair(*cert, *key)
	if err != nil {
		log.Fatal(err)
	}

	wh, err := newWebHook(*kubeConfig)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/mutate", wh)

	server := http.Server{
		Addr:      fmt.Sprintf(":%d", *port),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{certificate}},
		Handler:   mux,
	}

	exitChan := make(chan struct{})

	go func() {
		err := server.ListenAndServeTLS("", "")
		select {
		case <-exitChan:
		default:
			log.Println(err)
			close(exitChan)
		}
	}()

	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-exitChan:
	case <-sigChan:
		close(exitChan)
		server.Shutdown(context.Background())
	}
}
