package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tlwr/enphase-gateway-exporter/pkg/enphase"
)

var (
	gatewayIP = flag.String("gateway-ip", "", "adres van de gateway")
	username  = flag.String("username", "", "gebruikersnaam / email voor enphase/enlighten")
	password  = flag.String("password", "", "wachtwoord voor enphase/enlighten")
	serial    = flag.String("serial", "", "serial enphase gateway")

	scrapeInterval = flag.Duration("scrape-interval", 30*time.Second, "interval voor metrics halen")
	scrapeTimeout  = flag.Duration("scrape-timeout", 20*time.Second, "timeout voor metrics halen")
	promAddr       = flag.String("prometheus-addr", ":9365", "listen adres voor prometheus metrics")
)

func main() {
	flag.Parse()

	rootCtx := context.Background()
	sigCtx, sigStop := signal.NotifyContext(rootCtx, os.Interrupt, syscall.SIGTERM)
	defer sigStop()

	if len(*gatewayIP) == 0 {
		log.Fatalf("gateway ip is verplicht")
	}

	if len(*username) == 0 {
		log.Fatalf("username is verplicht")
	}

	if len(*password) == 0 {
		log.Fatalf("password is verplicht")
	}

	if len(*serial) == 0 {
		log.Fatalf("serial is verplicht")
	}

	gwc := enphase.NewGateway(*gatewayIP)
	etm := enphase.NewManager(*username, *password, *serial)
	go func() {
		if err := etm.Start(sigCtx); err != nil {
			log.Fatalf("error starting enphase token manager: %s", err)
		}
	}()

	etmTokenWaitCtx, etmtwCancel := context.WithTimeout(sigCtx, 1*time.Minute)
	defer etmtwCancel()
	if err := etm.WaitForToken(etmTokenWaitCtx); err != nil {
		log.Fatalf("error waiting for enphase token: %s", err)
	}

	registry := prometheus.NewRegistry()

	opgewektVandaag := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "opgewekt_vandaag_wh",
		Help: "Hoeveel stroom werdt opgewekt tot nu toe, vandaag",
	})
	registry.MustRegister(opgewektVandaag)
	opgewekt7d := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "opgewekt_7d_wh",
		Help: "Hoeveel stroom werdt opgewekt tot nu toe, laatste 7 dagen",
	})
	registry.MustRegister(opgewekt7d)
	opgewektLevensduur := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "opgewekt_levensduur_wh",
		Help: "Hoeveel stroom werdt opgewekt tot nu toe, levensduur van het apparaat",
	})
	registry.MustRegister(opgewektLevensduur)
	huidigeStroom := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "huidige_stroom_w",
		Help: "Hoevel stroom nu wordt geproduceerd",
	})
	registry.MustRegister(huidigeStroom)

	wg := sync.WaitGroup{}

	promServer := http.Server{
		Addr: *promAddr,
		Handler: promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{
				EnableOpenMetrics: false,
			}),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		log.Printf("prometheus starten: %s", *promAddr)
		err := promServer.ListenAndServe() //nolint:gosec // Ignoring G114: Use of net/http serve function that has no support for setting timeouts.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("error prometheus server: %v", err)
			return
		}
	}()

	scrape := func() {
		log.Println("metrics aan het halen")
		defer log.Println("klaar met metrics halen")

		scrapeCtx, scrapeCancel := context.WithTimeout(sigCtx, *scrapeTimeout)
		defer scrapeCancel()

		production, err := gwc.ScrapeProduction(scrapeCtx, etm.GetToken())
		if err != nil {
			log.Printf("kon geen metrics halen van de gateway: %s", err)
		} else {
			opgewektVandaag.Set(production.Wh)
			opgewekt7d.Set(production.Wh7d)
			opgewektLevensduur.Set(production.WhL)
			huidigeStroom.Set(production.W)
		}
	}

	wg.Add(1)
	go func() {
		defer log.Println("klaar met draaien")
		defer wg.Done()

		runImmediately := time.After(0)
		period := time.NewTicker(*scrapeInterval)

		for {
			select {
			case <-sigCtx.Done():
				return
			case <-runImmediately:
				scrape()
			case <-period.C:
				scrape()
			}
		}
	}()

	<-sigCtx.Done()
	sigStop()
	log.Println("signaal te stoppen ontvangen")

	psCtx, psCancel := context.WithTimeout(rootCtx, 15*time.Second)
	defer psCancel()

	if err := promServer.Shutdown(psCtx); err != nil {
		log.Fatalf("kon niet prometheus server uitschakkelen: %s", err)
	} else {
		log.Println("prometheus gestopd")
	}
	psCancel()

	wg.Wait()
	log.Println("exit")
}
