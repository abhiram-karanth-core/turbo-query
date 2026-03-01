package main

import (
	"log"

	"turbo-query/internal/server"
)

func main() {
	srv := server.NewServer()

	log.Println("starting shard server on", srv.Addr)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}