package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/johanbrandhorst/redeploy/config"
	"github.com/johanbrandhorst/redeploy/handler"
)

var port = flag.String("port", "8555", "The port to serve on.")
var host = flag.String("host", "", "The local address to serve on.")
var confFile = flag.String("config", "services.yaml", "The configuration file to use.")
var path = flag.String("path", "", "The path to serve Docker Hub webhooks on. If unspecified, serves on /.")
var tlsCert = flag.String("tls-cert", "", "The x509 certificate to serve with, in PEM format. Optional.")
var tlsKey = flag.String("tls-key", "", "The private key to serve with, in PEM format. Optional.")
var logLevel = flag.Int("log-level", int(logrus.InfoLevel), "Logrus log level to use. 0 is Panic, 5 is Debug.")

func main() {
	flag.Parse()

	conf, err := config.LoadConfig(*confFile)
	if err != nil {
		log.Fatalln("Failed to parse config:", err)
	}

	log := logrus.New()
	log.Level = logrus.Level(*logLevel)
	log.Formatter = &logrus.TextFormatter{
		TimestampFormat: time.RFC3339,
		ForceColors:     true,
	}

	hook, err := handler.New(conf, handler.WithLogger(log))
	if err != nil {
		log.Fatalln("Failed to create Docker hook:", err)
	}

	http.Handle("/"+*path, hook)

	srv := &http.Server{
		Addr:    net.JoinHostPort(*host, *port),
		Handler: http.DefaultServeMux,
	}

	go func() {
		var err error
		if *tlsCert != "" && *tlsKey != "" {
			log.Print("Serving on https://", srv.Addr, "/"+*path)
			err = srv.ListenAndServeTLS(*tlsCert, *tlsKey)
		} else {
			log.Print("Serving on http://", srv.Addr, "/"+*path)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalln("Failed to serve:", err)
		}
	}()

	cancel := make(chan os.Signal)
	signal.Notify(cancel, syscall.SIGTERM, syscall.SIGINT)
	<-cancel
	err = srv.Shutdown(context.Background())
	if err != nil {
		log.Fatalln("Failed to shut down:", err)
	}

	log.Println("Shut down gracefully")
}
