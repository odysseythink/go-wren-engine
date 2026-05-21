package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/server"
	"github.com/wren-engine/wren/internal/service"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "config file path (properties or yaml)")
	flag.Parse()

	if configPath == "" {
		configPath = os.Getenv("WREN_CONFIG_FILE")
	}
	if configPath == "" {
		log.Fatalf("--config or WREN_CONFIG_FILE env required")
	}

	configMgr := config.NewConfigManager()
	if err := configMgr.LoadFromFile(configPath); err != nil {
		log.Fatalf("Config file not found: %v", err)
	}
	configMgr.LoadFromEnv()

	md := duckdb.NewMetadata()
	var metadata service.Metadata = md
	var sqlConverter converter.SqlConverter = &converter.DuckDBSqlConverter{}

	previewService := service.NewPreviewService(metadata, sqlConverter, configMgr)
	validationService := service.NewValidationService(metadata, sqlConverter)

	srv := server.NewServer(fmt.Sprintf(":%d", configMgr.Port()))

	mdlHandler := server.NewMDLHandler(previewService, validationService)
	mdlHandler.RegisterRoutes(srv.Router())

	analysisHandler := server.NewAnalysisHandler()
	analysisHandler.RegisterRoutes(srv.Router())

	duckdbHandler := server.NewDuckDBHandler(md)
	duckdbHandler.RegisterRoutes(srv.Router())

	configHandler := server.NewConfigHandler(configMgr)
	configHandler.RegisterRoutes(srv.Router())

	fmt.Printf("wren-engine starting on port %d...\n", configMgr.Port())
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
