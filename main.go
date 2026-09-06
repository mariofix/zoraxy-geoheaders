package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mariofix/zoraxy-geoheaders/mod/api"
	"github.com/mariofix/zoraxy-geoheaders/mod/config"
	"github.com/mariofix/zoraxy-geoheaders/mod/geoip"
	"github.com/mariofix/zoraxy-geoheaders/mod/proxy"
	"github.com/mariofix/zoraxy-geoheaders/mod/stats"
	"github.com/mariofix/zoraxy-geoheaders/mod/zoraxy_plugin"
)

//go:embed www/*
var embeddedWeb embed.FS

const (
	PluginVersion = "1.0.0"
	PluginName    = "GeoHeaders"
	PluginAuthor  = "mariofix"
	PluginTag     = "geo-match"
)

var (
	flagPort       = flag.String("port", ":8080", "Port to listen on (e.g. :8080 or 8080)")
	flagConfig     = flag.String("config", "config.json", "Path to config file")
	flagDev        = flag.Bool("dev", false, "Enable development mode (load UI from disk)")
	flagVersion    = flag.Bool("version", false, "Print plugin version and exit")
	flagIntrospect = flag.Bool("introspect", false, "Print plugin introspection JSON and exit")
	flagConfigure  = flag.String("configure", "", "Zoraxy configure payload (base64 JSON)")
)

func main() {
	flag.Parse()

	if *flagVersion {
		fmt.Printf("%s v%s by %s\n", PluginName, PluginVersion, PluginAuthor)
		os.Exit(0)
	}

	spec := zoraxy_plugin.IntrospectSpec{
		Name:               PluginName,
		Author:             PluginAuthor,
		Version:            PluginVersion,
		Type:               zoraxy_plugin.PluginTypeRouter,
		TargetTag:          PluginTag,
		Description:        "Inject X-Country in every request for tagged (geo-match) endpoints",
		DynamicRouter:      true,
		AllowConfiguration: true,
	}

	if *flagIntrospect {
		zoraxy_plugin.HandleIntrospectFlag(spec)
		os.Exit(0)
	}

	listenPort := *flagPort
	rootURLPath := "/"
	configFile := *flagConfig

	// Parse configure flag if launched by Zoraxy
	if *flagConfigure != "" {
		cfgSpec, err := zoraxy_plugin.ParseConfigureFlag(*flagConfigure)
		if err != nil {
			log.Printf("[GeoHeaders] Warning: failed to parse configure spec: %v", err)
		} else {
			if cfgSpec.ListeningPort > 0 {
				listenPort = fmt.Sprintf(":%d", cfgSpec.ListeningPort)
			}
			if cfgSpec.WebRootPath != "" {
				rootURLPath = cfgSpec.WebRootPath
			}
			if cfgSpec.RuntimeFolder != "" {
				configFile = filepath.Join(cfgSpec.RuntimeFolder, "config.json")
			}
			log.Printf("[GeoHeaders] Initialized via Zoraxy configure: port=%s, webRoot=%s, folder=%s",
				listenPort, rootURLPath, cfgSpec.RuntimeFolder)
		}
	}

	if !strings.HasPrefix(listenPort, ":") {
		listenPort = ":" + listenPort
	}

	// Initialize Configuration
	cfgMgr, err := config.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("[GeoHeaders] Failed to load config: %v", err)
	}
	cfg := cfgMgr.Get()

	// Initialize GeoIP Engine
	geoDB := geoip.NewGeoDB(&geoip.GeoDBOptions{
		FallbackCountryCode: cfg.FallbackCountry,
		LocalCountryCode:    cfg.LocalCountry,
	})

	// Load custom overrides from config
	for cidr, country := range cfg.CustomOverrides {
		_ = geoDB.AddCustomOverride(cidr, country)
	}

	// Initialize Stats Tracker
	tracker := stats.NewTracker(cfg.LogLimit)

	// Initialize Proxy Engine
	proxyEngine := proxy.NewProxyEngine(cfgMgr, geoDB, tracker)

	// Initialize API Handler
	apiHandler := api.NewAPIHandler(cfgMgr, geoDB, tracker, PluginVersion)

	// Setup Router
	mux := http.NewServeMux()

	// Register Zoraxy Dynamic Sniff & Capture endpoints
	zoraxy_plugin.RegisterDynamicSniffHandler(mux, proxyEngine.HandleSniff)
	zoraxy_plugin.RegisterDynamicCaptureHandler(mux, proxyEngine.HandleCapture)

	// Register API Routes
	apiHandler.RegisterRoutes(mux, rootURLPath)

	// Register Web UI Router
	uiRouter := zoraxy_plugin.NewPluginEmbedUIRouter("geoheaders", &embeddedWeb, "www", rootURLPath)
	if *flagDev {
		uiRouter.SetDevWebRoot("www")
	}

	// Graceful termination handler
	server := &http.Server{
		Addr:    listenPort,
		Handler: mux,
	}

	uiRouter.RegisterTerminateHandler(func() {
		log.Println("[GeoHeaders] Termination requested by Zoraxy host")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}, mux)

	// Mount UI router as fallback handler for root / webRoot
	cleanRoot := rootURLPath
	if !strings.HasSuffix(cleanRoot, "/") {
		cleanRoot += "/"
	}
	mux.Handle(cleanRoot, uiRouter.Handler())

	// Handle OS interrupt signals
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[GeoHeaders] Starting Zoraxy GeoHeaders Plugin v%s on %s (Web UI: %s)",
			PluginVersion, listenPort, rootURLPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[GeoHeaders] HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("[GeoHeaders] Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	log.Println("[GeoHeaders] Stopped.")
}
