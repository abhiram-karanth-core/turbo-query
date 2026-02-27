package main

import (
	"fmt"
	"path/filepath"
	"sync"
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

	baseDir := "data"

	for i := 0; i < numShards; i++ {
		shardDir := filepath.Join(baseDir, fmt.Sprintf("shard-%d", i))

		vecFile, mmapBuf, err := initShardStorage(baseDir, i)
		if err != nil {
			panic(err)
		}

		index, err := initBleve(shardDir)
		if err != nil {
			panic(err)
		}

		shards[i] = &Shard{
			ID:        i,
			Index:     index,
			NextDocID: 0,
			DocMap:    make(map[string]uint32),
			VecFile:   vecFile,
			Mmap:      mmapBuf,
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
	for i := 0; i < numShards; i++ {
		shardDir := filepath.Join(baseDir, fmt.Sprintf("shard-%d", i))

		// flush docmap
		if err := saveDocMap(shardDir, shards[i].DocMap); err != nil {
			fmt.Println("docmap save error:", err)
		}
		// unmap
		shards[i].Mmap.Unmap()
		// close vector file
		shards[i].VecFile.Close()
		// close bleve
		shards[i].Index.Close()
	}
	fmt.Println("Indexing complete")
}
