package main

import (
	"log"

	"turbo-query/internal/embed"
	"turbo-query/internal/server"
)

func main() {
	if err := embed.Init(); err != nil {
		log.Fatalf("failed to init embedding model: %v", err)
	}

	srv := server.NewServer()

	log.Println("starting shard server on", srv.Addr)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}