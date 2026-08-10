// Command diag adalah alat verifikasi sementara: server di port 8001.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/providers/deepseek"
	"fakemodelapi/internal/providers/dummy"
	"fakemodelapi/internal/server"
)

func main() {
	provider.Register("dummy", dummy.New())
	provider.Register("deepseek", deepseek.New())

	srv := server.New("deepseek", 8001)
	if err := srv.Start(); err != nil {
		log.Fatalf("gagal start: %v", err)
	}
	log.Printf("diag server di port 8001")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	_ = srv.Stop()
}
