# Turbo Query — Distributed Hybrid Search Engine

Turbo Query is a high-performance distributed search system that combines BM25 keyword retrieval with semantic vector search using MiniLM embeddings. The project demonstrates production-style search infrastructure concepts including sharding, fan-out querying, mmap vector storage, and hybrid ranking.

> **Note:** This checkpoint covers per-shard querying only. Parallel coordinator fan-out is not yet implemented.

---

## Features

- **Hybrid search:** BM25 + semantic reranking
- **MiniLM embeddings** via Ollama
- **Memory-mapped vector store** for zero-copy reads
- **Consistent hash–based sharding**
- **Per-shard HTTP servers** (Go + chi)
- **Fully containerized** with Docker Compose
- **Per-shard latency instrumentation**
- **Robust handling** of embedding and out-of-bounds edge cases

---

## Architecture

```
User Query
    ↓
Coordinator (fan-out — pending)
    ↓
Shard Servers (parallel)
    ↓
BM25 retrieve → Vector rerank → Hybrid score
    ↓
Top-K merge → Response
```

Each shard maintains:

- A **Bleve** inverted index for keyword retrieval
- A **memory-mapped** dense vector store (`vectors.bin`)
- A shard-local document ID mapping
- An HTTP search endpoint

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go |
| HTTP Router | chi |
| Full-text Search | Bleve |
| Embeddings | all-MiniLM via Ollama |
| Containerization | Docker & Docker Compose |
| Vector Storage | mmap-backed binary file |

---

## Project Structure

```
cmd/
  api/            # coordinator server (fan-out layer)
  shard/          # per-shard search server

internal/
  embed/          # embedding client + normalization
  shardnode/      # shard HTTP handlers and hybrid search
  data/           # shard indexes and vector files

indexer/          # offline indexing pipeline
```

---

## Indexing Pipeline

The offline indexer performs:

1. Wiki JSON ingestion
2. MiniLM embedding generation
3. Vector normalization and mmap write
4. Consistent hash routing to shards
5. Bleve batch indexing
6. DocID alignment persistence

> Document and query vectors are L2-normalized so cosine similarity can be computed efficiently via dot product.

---

## Scoring Strategy

Each shard performs:

1. BM25 candidate retrieval
2. Dense vector similarity (cosine via dot product)
3. Score normalization
4. Hybrid fusion:

```
final_score = 0.7 * BM25_norm + 0.3 * cosine_norm
```

---

## Performance Notes

- Typical shard latency per shard: **~10–15 ms** (local)
- Embedding generation dominates query cost
- `mmap` enables zero-copy vector access
- Hybrid reranking window is configurable

---

## Current Status

### Completed

- Shard-local hybrid search
- Vector mmap store
- Bleve field hydration
- Embedding pipeline hardening
- Per-shard latency logging

### In Progress

- Distributed coordinator fan-out
- Cross-shard top-K merge
- Tail-latency analysis
- Query result caching

---

## Future Improvements

- Coordinator fan-out and result merging
- Document chunking for long texts
- ANN acceleration (HNSW / IVF)
- Query result caching
- Adaptive hybrid weighting
