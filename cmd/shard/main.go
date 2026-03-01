package main

import (
	"log"

	"turbo-query/internal/shardnode"
)

func main() {
	srv := shardnode.NewServer()

	log.Println("starting shard server on", srv.Addr)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}