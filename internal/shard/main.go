package main

import (
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
)

var shardWg sync.WaitGroup

func main() {
	numShards := 4
	numWorkers := 4
	vnodes := 128

	//hash ring
	ring := NewHashRing(numShards, vnodes)

	jobs := make(chan IndexJob, 1000) // channels
	prepared := make(chan PreparedDoc, 1000)

	shardChans := make([]chan PreparedDoc, numShards)
	shards := make([]*Shard, numShards)

	for i := 0; i < numShards; i++ {
		mapping := bleve.NewIndexMapping()
		index, err := bleve.NewMemOnly(mapping)
		if err != nil {
			panic(err)
		}

		shards[i] = &Shard{
			ID:        i,
			Index:     index,
			NextDocID: 0,
			DocMap:    make(map[string]uint32),
			Mmap:      make([]byte, 1024*1024*100), // TEMP fake mmap
			Batch:     index.NewBatch(),
		}

		shardChans[i] = make(chan PreparedDoc, 1000)

		shardWg.Add(1)
		go func(s *Shard, ch chan PreparedDoc) {
			defer shardWg.Done()
			shardWriter(s, ch)
		}(shards[i], shardChans[i])
	}
	var workerWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			worker(jobs, prepared)
		}()
	}
	go func() {
		workerWg.Wait()
		close(prepared)
	}()
	go router(ring, prepared, shardChans)

	go func() {
		err := ingestWiki("wiki_120k.json", jobs)
		if err != nil {
			fmt.Println("ingest error:", err)
		}
		close(jobs)
	}()

	shardWg.Wait()
	fmt.Println("Indexing complete")
}
