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
| Embeddings | all-MiniLM-L6-v2 via Ollama |
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
  embed/          # embedding client + L2 normalization
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

Benchmarked on a single host (WSL2, 4-core, 10GB RAM) using [wrk2](https://github.com/giltene/wrk2) — a constant-rate load generator that corrects for coordinated omission.

### Summary

| Mode | RPS | Cache Hit Rate | p50 | p99 | Bottleneck |
|---|---|---|---|---|---|
| Redis warm (Zipfian load) | 10,000 | ~99% | 1.3ms | 3ms | None |
| Realistic mixed load | 100 | ~56% | 15ms | 228ms | Ollama embeddings |
| No cache | ~50 | 0% | 35ms | 1.3s | Ollama embeddings |

### Redis Warm — 10k RPS

Under Zipfian query distribution (realistic search traffic), the top queries populate Redis within seconds. Once warm, the cache absorbs ~99% of requests.

```bash
./wrk -t8 -c50 -d30s -R 10000 --latency -s search.lua http://localhost:8080/search
```

| Percentile | Cold (page cache dropped) | Warm |
|---|---|---|
| p50 | 1.30ms | 1.30ms |
| p75 | 1.72ms | 1.72ms |
| p90 | 2.11ms | 2.09ms |
| p95 | 2.34ms | 2.31ms |
| p99 | 3.01ms | 2.86ms |
| p99.9 | 23.84ms | 4.43ms |
| max | 51.58ms | 11.18ms |

> Cold p99.9 of 24ms reflects first-access mmap page faults on the vector store — a one-time startup cost that disappears as pages warm in the OS page cache.

### Realistic Mixed Load — 100 RPS

With a diverse query pool (~1,000 unique combinations), Redis hit rate drops to ~56%. The remaining 44% of requests execute the full pipeline including Ollama embedding generation.

```bash
./wrk -t8 -c50 -d30s -R 100 --latency -s search.lua http://localhost:8080/search
```

| Percentile | Latency |
|---|---|
| p50 | 15ms |
| p90 | 34ms |
| p95 | 40ms |
| p99 | 228ms |
| max | 353ms |

The p50 of 15ms reflects the blend of ~1ms Redis hits and ~35ms Ollama misses. The system is stable at 100 RPS — the hard ceiling is single-instance Ollama at ~50 embeddings/sec.

### Latency Breakdown (cache miss)

| Component | Latency |
|---|---|
| Redis cache hit | ~1–2ms |
| Ollama embedding (MiniLM, model warm) | ~30–35ms |
| BM25 + parallel fan-out + rerank | ~3–5ms |
| **Cache miss total** | **~35–40ms** |

### Scaling Beyond Single-Host

The search pipeline itself — BM25, mmap vector reads, fan-out, rerank — sustains sub-5ms p99 at high concurrency. The embedding layer is the only throughput bottleneck. In a production deployment:

- Multiple Ollama instances behind a load balancer increase embedding throughput linearly
- Shards are stateless and scale horizontally
- Zipfian query distributions mean Redis absorbs the majority of load, keeping embedding cost amortized

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