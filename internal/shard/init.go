package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
	mmap "github.com/edsrzf/mmap-go"
)

func initShardStorage(baseDir string, shardID int) (*os.File, mmap.MMap, error) {
	shardDir := filepath.Join(baseDir, fmt.Sprintf("shard-%d", shardID))

	// create shard directory
	if err := os.MkdirAll(shardDir, 0755); err != nil {
		return nil, nil, err
	}

	vecPath := filepath.Join(shardDir, "vectors.bin")

	file, err := os.OpenFile(vecPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, nil, err
	}

	// pre-size file
	size := int64(maxDocsPerShard * vectorBytes)
	if err := file.Truncate(size); err != nil {
		file.Close()
		return nil, nil, err
	}

	// mmap the file
	mm, err := mmap.MapRegion(
		file,
		int(size),
		mmap.RDWR,
		0,
		0,
	)
	if err != nil {
		file.Close()
		return nil, nil, err
	}

	return file, mm, nil
}

func initBleve(shardDir string) (bleve.Index, error) {
	indexPath := filepath.Join(shardDir, "index.bleve")

	titleField := bleve.NewTextFieldMapping()
	titleField.Store = true

	textField := bleve.NewTextFieldMapping()
	textField.Store = true


	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("title", titleField)
	docMapping.AddFieldMappingsAt("text", textField)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping

	return bleve.New(indexPath, indexMapping)
}
func saveDocMap(shardDir string, docMap map[string]uint32) error {
	path := filepath.Join(shardDir, "docmap.json")

	// invert map to localID -> globalID
	inv := make(map[uint32]string, len(docMap))
	for gid, lid := range docMap {
		inv[lid] = gid
	}

	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
