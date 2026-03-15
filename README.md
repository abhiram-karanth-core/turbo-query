# Turbo Query — Distributed Hybrid Search Engine

Turbo Query is a high-performance distributed search system that combines BM25 keyword retrieval with semantic vector search using MiniLM embeddings. The project demonstrates production-style search infrastructure concepts including sharding, fan-out querying, mmap vector storage, Redis query-result caching, and hybrid ranking.

---

## Features

- **Hybrid search:** BM25 + semantic reranking via MiniLM embeddings
- **Memory-mapped vector store** for zero-copy reads
- **Redis query-result caching** — primary performance lever under realistic load
- **Consistent hash–based sharding** across 4 shard nodes
- **Parallel fan-out** coordinator with per-shard HTTP servers (Go + chi)
- **Fully containerized** with Docker Compose
- **300,000+ documents** indexed across 4 shards (~75k docs/shard)

---

## Architecture

```
User Query
    ↓
Redis Query Cache
    ↓ (cache miss)
Coordinator (parallel fan-out)
    ↓
Shard 0  Shard 1  Shard 2  Shard 3
    ↓
BM25 retrieve → Vector rerank → Hybrid score
    ↓
Top-K merge → Cache result in Redis → Response
```

Each shard maintains:
- A **Bleve** inverted index for BM25 keyword retrieval
- A **memory-mapped dense vector store** (`vectors.bin`) for zero-copy vector reads
- **Shard-local sequential document IDs** used as direct offsets into the vector file

### Caching

Redis caches full query results at the coordinator. Cache hits bypass the entire search pipeline — no embedding, no fan-out, no rerank.

- **Cache hit:** ~1–2ms regardless of query complexity
- **Cache miss:** full pipeline — embedding → fan-out → BM25 → rerank → merge

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go |
| HTTP Router | chi |
| Inverted Index / BM25 | Bleve |
| Embeddings | all-MiniLM-L6-v2 via ONNX Runtime (hugot) |
| Query Cache | Redis |
| Containerization | Docker Compose |
| Vector Storage | mmap-backed binary file |

---

## Project Structure

```
cmd/
  api/            # coordinator server (fan-out + cache layer)
  shard/          # per-shard search server

internal/
  embed/          # ONNX Runtime embedding client + L2 normalization
  shardnode/      # shard HTTP handlers and hybrid search logic
  data/           # shard indexes and vector files

indexer/          # offline indexing pipeline
```

---

## Indexing Pipeline

1. Wiki JSON ingestion
2. MiniLM embedding generation
3. L2 vector normalization and mmap write
4. Consistent hash routing to shards
5. Bleve batch indexing
6. Sequential shard-local ID assignment for direct vector lookup

> Vectors are L2-normalized at index time so cosine similarity reduces to a dot product at query time.

---

## Scoring

Each shard performs:

1. BM25 candidate retrieval (top 100)
2. Cosine similarity via dot product against mmap-backed vectors
3. Score normalization
4. Hybrid fusion:

```
final_score = 0.7 × BM25_norm + 0.3 × cosine_norm
```

> Reranking runs only over BM25 top-100 candidates — cost is constant regardless of shard size.

---

## Performance

Benchmarked on a laptop (WSL2, i7-13650HX, 12GB RAM) using [wrk2](https://github.com/giltene/wrk2) — a constant-rate load generator that corrects for coordinated omission.

### Summary

| Mode | RPS | Cache Hit Rate | p50 | p99 | Bottleneck |
|---|---|---|---|---|---|
| Redis warm (Zipfian load) | 10,000 | ~99% | 1.4ms | 3ms | None |
| Full pipeline (no cache) | ~100 | 0% | 128ms | 956ms | ORT single-session mutex |

### Redis Warm — 10k RPS

Under Zipfian query distribution (realistic search traffic), the top queries populate Redis within seconds. Once warm, the cache absorbs ~99% of requests.

```bash
./wrk -t8 -c50 -d30s -R 10000 --latency -s search.lua http://localhost:8080/search
```

| Percentile | Latency |
|---|---|
| p50 | 1.40ms |
| p75 | 1.82ms |
| p90 | 2.22ms |
| p99 | 3.02ms |
| p99.9 | 4.39ms |
| max | 9.97ms |

### Full Pipeline — No Cache

With salt appended to every query, every request executes the full pipeline: embed → fan-out → BM25 → rerank → merge. No Redis hits.

```bash
docker exec turbo-query-redis-1 redis-cli FLUSHALL
./wrk -t8 -c50 -d30s -R 200 --latency -s search.lua http://localhost:8080/search
```

| Percentile | Latency |
|---|---|
| p50 | 128ms |
| p90 | 184ms |
| p99 | 956ms |
| max | 1.1s |

The p50 of 128ms under 50 concurrent connections reflects mutex queuing at the embed layer — with 50 goroutines contending for one ORT session at ~3ms per embed, average wait is ~50 × 3ms = 150ms. Sequential single-request latency is 15–20ms.

### Latency Breakdown (single request, no contention)

| Component | Latency |
|---|---|
| Redis cache hit | ~1–2ms |
| ONNX Runtime embedding (MiniLM) | ~2–4ms |
| BM25 + parallel fan-out + rerank | ~7–30ms |
| **Cache miss total (sequential)** | **~15–20ms** |
| **Cache miss total (concurrent, mutex queuing)** | **~128ms p50** |

### Embedding: ONNX Runtime vs Ollama

Switching from Ollama to direct ONNX Runtime inference via hugot reduced per-query embedding latency from ~50–200ms to ~2–4ms — a 25–50x improvement. The hugot library runs MiniLM-L6-v2 directly through ORT without generative LLM overhead.

| Backend | Embed latency | Full pipeline ceiling |
|---|---|---|
| Ollama | ~50–200ms | ~50 RPS |
| ONNX Runtime (hugot) | ~2–4ms | ~100 RPS |

---

## Known Limitations

The hugot ONNX Runtime backend enforces a single active session globally — attempting to create multiple sessions crashes with `another session is currently active`. All embedding calls serialize through a mutex, limiting full-pipeline throughput to ~100 RPS under concurrent load.

The correct fix is migrating to [`yalue/onnxruntime_go`](https://github.com/yalue/onnxruntime_go) which supports multiple independent sessions. A pool of 8 sessions with 2 intra-op threads each would be expected to unlock 500–1000 RPS full pipeline on this hardware.

The shard pipeline itself — BM25, mmap vector reads, parallel fan-out, hybrid rerank — is not the bottleneck. All 4 shards respond within ~7–30ms under load.

---

## Querying the API

```bash
curl -s -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"byzantine empire fall","top_k":5}' \
  | python -m json.tool
```

### Example Response

```json
[
  {
    "doc_id": "14823",
    "score": 0.9341,
    "shard_id": "2",
    "title": "Fall of Constantinople",
    "text": "The fall of Constantinople in 1453 marked the end of the Byzantine Empire..."
  },
  {
    "doc_id": "9217",
    "score": 0.8976,
    "shard_id": "0",
    "title": "Byzantine Empire",
    "text": "The Byzantine Empire, also known as the Eastern Roman Empire..."
  }
]
```