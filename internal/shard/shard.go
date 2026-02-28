package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"sort"

	"turbo-query/internal/embed"
	"unsafe"

	"github.com/blevesearch/bleve/v2"
	mmap "github.com/edsrzf/mmap-go"
)

type Shard struct {
	ID        int
	Index     bleve.Index
	NextDocID uint32
	DocMap    map[string]uint32
	VecFile   *os.File
	Mmap      mmap.MMap
	Batch     *bleve.Batch
}

type IndexJob struct {
	ID    string
	Title string
	Text  string
}

type WikiDoc struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type PreparedDoc struct {
	GlobalID string
	Title    string
	Text     string
	Vector   []float32
}
type HashRing struct {
	positions []uint32       // sorted
	shardMap  map[uint32]int // position -> shardID
}

const (
	vectorDim       = embed.Dim
	vectorBytes     = vectorDim * 4
	maxDocsPerShard = 70000
)

func hash32(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

func float32SliceToBytes(f []float32) []byte {
	if len(f) == 0 {
		return nil
	}
	return unsafe.Slice(
		(*byte)(unsafe.Pointer(unsafe.SliceData(f))),
		len(f)*4,
	)
}
func NewHashRing(numShards int, vnodes int) *HashRing {
	r := &HashRing{
		shardMap: make(map[uint32]int),
	}

	for shardID := 0; shardID < numShards; shardID++ {
		for v := 0; v < vnodes; v++ {

			key := fmt.Sprintf("shard-%d-vnode-%d", shardID, v)
			pos := hash32(key)

			r.positions = append(r.positions, pos)
			r.shardMap[pos] = shardID
		}
	}

	sort.Slice(r.positions, func(i, j int) bool {
		return r.positions[i] < r.positions[j]
	})

	return r
}
func (r *HashRing) ShardFor(key string) int {
	h := hash32(key)

	// binary search
	idx := sort.Search(len(r.positions), func(i int) bool {
		return r.positions[i] >= h
	})

	// wrap around ring
	if idx == len(r.positions) {
		idx = 0
	}

	pos := r.positions[idx]
	return r.shardMap[pos]
}

func ingestWiki(path string, jobs chan<- IndexJob) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// IMPORTANT: increase buffer for wiki
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var doc WikiDoc
		if err := json.Unmarshal(line, &doc); err != nil {
			continue
		}

		jobs <- IndexJob{
			ID:    doc.ID,
			Text:  doc.Text,
			Title: doc.Title,
		}
	}

	return scanner.Err()
}

func worker(jobs <-chan IndexJob, out chan<- PreparedDoc) {
	for job := range jobs {
		vec := embed.Embed(job.Title + " " +job.Text)

		out <- PreparedDoc{
			GlobalID: job.ID,
			Title:    job.Title,
			Text:     job.Text,
			Vector:   vec,
		}
		// fmt.Println("embedded", job.ID)
	}
}

func router(ring *HashRing, in <-chan PreparedDoc, shardChans []chan PreparedDoc) {
	for doc := range in {
		shardID := ring.ShardFor(doc.GlobalID)
		shardChans[shardID] <- doc
	}
	for _, ch := range shardChans {
		close(ch)
	}
}
func writeVector(s *Shard, id uint32, vec []float32) {
	dim := embed.Dim

	offset := int64(id) * int64(dim*4)
	size := int64(dim * 4)
	end := offset + size
	if end > int64(len(s.Mmap)) {
		panic("mmap overflow")
	}
	copy(s.Mmap[offset:end], float32SliceToBytes(vec))
}

const batchSize = 100

func shardWriter(s *Shard, ch <-chan PreparedDoc) {
	for doc := range ch {

		localID := s.NextDocID
		s.NextDocID++

		writeVector(s, localID, doc.Vector)

		s.DocMap[doc.GlobalID] = localID

		s.Batch.Index(doc.GlobalID, map[string]interface{}{
			"title":doc.Title,
			"text": doc.Text,
		})

		if s.Batch.Size() >= batchSize {
			s.Index.Batch(s.Batch)
			s.Batch = s.Index.NewBatch()
		}

		fmt.Println("shard", s.ID, "indexed", doc.GlobalID)
	}
	if s.Batch.Size() > 0 {
		s.Index.Batch(s.Batch)
	}
}
