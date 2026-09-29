package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"eidosinventory/internal/web"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func main() {
	dataDir := getenv("DATA_DIR", "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "uploads"), 0700); err != nil {
		log.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "inventory.db")), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	server, err := web.New(db, dataDir)
	if err != nil {
		log.Fatal(err)
	}
	addr := getenv("ADDR", "127.0.0.1:18080")
	log.Printf("Eidos库存台账：http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, server))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
