# Kache
### **A distributed, persistent key-value store built from scratch in Go.**
Redis-inspired in-memory caching engine with SkipList indexing, Write-Ahead Logging for crash recovery, pluggable LRU/LFU eviction, a custom binary wire protocol, and multi-node Raft consensus for fault-tolerant replication.
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
---
## Why Kache?
Production key-value stores like Redis, etcd, and Memcached make specific trade-offs between performance, durability, and availability. Kache is built from first principles to deeply understand those trade-offs:
- **Why single-threaded command processing?** Avoids lock contention on the hot path. Redis made the same choice.
- **Why a SkipList instead of a hash map?** Supports O(log N) range queries alongside O(log N) point lookups. A hash map is O(1) for point lookups but can't do range scans.
- **Why Raft for consensus?** Provides strong consistency (linearizability) with well-understood failure modes, unlike leaderless or gossip-based approaches.
---
## Features
### Storage Engine
- **In-memory SkipList** — probabilistic data structure supporting O(log N) insert, search, delete, and range queries
- **Write-Ahead Log (WAL)** — append-only binary log on disk; every mutation is logged before being applied to memory, ensuring crash recovery
- **Snapshots** — periodic point-in-time serialization of the full in-memory state for fast recovery and WAL compaction
### Eviction & TTL
- **LRU (Least Recently Used)** — doubly-linked list + hash map for O(1) eviction
- **LFU (Least Frequently Used)** — frequency bucket list + hash map with O(1) eviction
- **Strategy Pattern** — eviction policies are hot-swappable at startup via a shared interface
- **TTL (Time-To-Live)** — per-key expiration with lazy + active expiry (min-heap + background goroutine)
### Networking
- **Custom binary protocol** — length-prefixed framing with magic bytes, CRC validation, and structured command/response serialization
- **Goroutine-per-connection** — Go's runtime multiplexes goroutines onto OS threads via a netpoller backed by epoll; ~2KB per goroutine enables 10K+ concurrent connections
- **Graceful shutdown** — context-based cancellation with connection draining
### Distributed Consensus (Raft)
- **Leader election** — randomized timeouts prevent split votes; majority quorum required
- **Log replication** — leader replicates entries to followers; commits on majority acknowledgment
- **State Pattern** — Leader, Follower, and Candidate states implement a shared interface; the node transitions between states based on election results and timeouts
- **gRPC transport** — Protobuf-defined RPCs for `AppendEntries` and `RequestVote` over HTTP/2
### Design Patterns
- **Command Pattern** — each operation (GET, SET, DEL, EXPIRE) is a self-contained command object, enabling WAL serialization and replay
- **Strategy Pattern** — eviction policies are interchangeable without modifying the storage engine
- **State Pattern** — Raft node roles (Leader, Follower, Candidate) encapsulate role-specific behavior behind a common interface
---
## Architecture
```
┌─────────────────────────────────────────────────────────┐
│                      Client (TCP)                       │
│              Custom Binary Protocol (RESP-like)         │
└──────────────────────┬──────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────┐
│                    TCP Server                            │
│           Goroutine-per-connection model                 │
│         (Go netpoller → epoll internally)               │
└──────────────────────┬──────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────┐
│                 Command Executor                         │
│        Parses frames → Command objects → Execute         │
│              (Command Pattern)                          │
└────────┬────────────────────────────┬───────────────────┘
         │                            │
         ▼                            ▼
┌─────────────────┐      ┌────────────────────────┐
│   KV Engine     │      │   WAL (Write-Ahead Log) │
│  ┌───────────┐  │      │  Append-only binary log  │
│  │ SkipList  │  │      │  Background fsync batch   │
│  │ O(log N)  │  │      │  CRC32 per entry          │
│  └───────────┘  │      └────────────┬───────────┘
│  ┌───────────┐  │                   │
│  │ Eviction  │  │                   ▼
│  │ LRU / LFU │  │      ┌────────────────────────┐
│  │ (Strategy)│  │      │      Snapshot            │
│  └───────────┘  │      │  Periodic full dump       │
│  ┌───────────┐  │      │  WAL compaction after     │
│  │ TTL Mgr   │  │      └────────────────────────┘
│  │ Min-heap  │  │
│  └───────────┘  │
└─────────────────┘
         ┌──────── Raft Consensus Layer ────────┐
         │                                       │
    ┌────┴────┐   ┌──────────┐   ┌────────────┐ │
    │ Leader  │──▶│ Follower │   │  Follower  │ │
    │ (Node 1)│   │ (Node 2) │   │  (Node 3)  │ │
    └────┬────┘   └────┬─────┘   └─────┬──────┘ │
         │             │               │         │
         └─────── gRPC (Protobuf) ─────┘         │
         └───────────────────────────────────────┘
```
For detailed architecture diagrams, see [docs/hld.md](docs/hld.md).
---
## Tech Stack
| Component | Technology | Why |
|---|---|---|
| Language | Go 1.22+ | Built-in concurrency (goroutines), excellent networking stdlib, production standard for infrastructure (etcd, CockroachDB, Consul) |
| Storage | Custom SkipList | O(log N) range queries, simpler than B-Tree, naturally supports lock-free concurrent reads |
| Persistence | Custom WAL + Snapshots | Append-only log for durability, snapshots for fast recovery and log compaction |
| Eviction | LRU / LFU (Strategy Pattern) | O(1) eviction, pluggable via interface |
| Protocol | Custom binary framing | Length-prefixed, no delimiter scanning, magic bytes for protocol validation |
| Consensus | Raft (from scratch) | Strong consistency, well-understood, decomposed into leader election + log replication + safety |
| Inter-node RPC | gRPC + Protobuf | Strong typing, HTTP/2 multiplexing, built-in deadlines and cancellation |
| Testing | Go `testing` + `-race` | Table-driven tests, built-in benchmarks, compile-time race detection |
---
## Project Structure
```
kache/
├── cmd/
│   └── kache-server/
│       └── main.go                 # Entry point, config, server bootstrap
├── internal/
│   ├── server/
│   │   └── server.go               # TCP listener, connection handling
│   ├── protocol/
│   │   ├── frame.go                # Binary frame encoding/decoding
│   │   ├── command.go              # Command parsing
│   │   └── response.go             # Response serialization
│   ├── engine/
│   │   ├── engine.go               # KV engine interface + implementation
│   │   ├── skiplist.go             # SkipList data structure
│   │   └── skiplist_test.go        # Table-driven tests + benchmarks
│   ├── eviction/
│   │   ├── policy.go               # EvictionPolicy interface (Strategy)
│   │   ├── lru.go                  # LRU implementation
│   │   ├── lfu.go                  # LFU implementation
│   │   └── eviction_test.go        # Eviction policy tests
│   ├── command/
│   │   ├── executor.go             # Command executor (Command Pattern)
│   │   ├── commands.go             # GET, SET, DEL, EXPIRE, PING
│   │   └── executor_test.go        # Command tests
│   ├── ttl/
│   │   ├── manager.go              # TTL tracking with min-heap
│   │   └── manager_test.go         # TTL tests
│   ├── wal/
│   │   ├── wal.go                  # Write-Ahead Log
│   │   ├── entry.go                # WAL entry format
│   │   ├── reader.go               # WAL replay for recovery
│   │   └── wal_test.go             # WAL tests
│   ├── snapshot/
│   │   ├── snapshot.go             # Point-in-time snapshot
│   │   └── snapshot_test.go        # Snapshot tests
│   ├── raft/
│   │   ├── raft.go                 # Raft state machine
│   │   ├── state.go                # State Pattern: Leader/Follower/Candidate
│   │   ├── log.go                  # Raft log (append-only, indexed)
│   │   ├── election.go             # Leader election
│   │   ├── replication.go          # Log replication
│   │   └── raft_test.go            # Raft correctness tests
│   └── grpc/
│       ├── proto/
│       │   └── raft.proto           # Protobuf definitions
│       ├── server.go               # gRPC server
│       └── client.go               # gRPC client
├── pkg/
│   └── client/
│       └── client.go               # Go client library
├── tests/
│   └── integration_test.go         # End-to-end integration tests
├── docs/
│   ├── hld.md                      # High-Level Design
│   ├── lld.md                      # Low-Level Design
│   └── trade-offs.md               # Design Trade-offs
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```
---
## Quick Start
### Prerequisites
- Go 1.22 or later
- Protobuf compiler (`protoc`) — for Raft gRPC (Phase 3)
### Build & Run (Single Node)
```bash
# Clone
git clone https://github.com/yourusername/kache.git
cd kache
# Build
go build -o kache-server ./cmd/kache-server/
# Run
./kache-server --port 6380 --eviction lru --max-memory 256MB
```
### Run a 3-Node Raft Cluster
```bash
# Terminal 1 — Node 1 (will become leader)
./kache-server --port 6380 --raft-port 7380 --node-id 1 \
  --peers "localhost:7381,localhost:7382"
# Terminal 2 — Node 2
./kache-server --port 6381 --raft-port 7381 --node-id 2 \
  --peers "localhost:7380,localhost:7382"
# Terminal 3 — Node 3
./kache-server --port 6382 --raft-port 7382 --node-id 3 \
  --peers "localhost:7380,localhost:7381"
```
### Client Usage
```bash
# Connect with the CLI client
./kache-cli --host localhost --port 6380
> SET user:1 '{"name": "alice"}' EX 3600
OK
> GET user:1
{"name": "alice"}
> DEL user:1
OK
> PING
PONG
```
---
## Supported Commands
| Command | Syntax | Description |
|---|---|---|
| `SET` | `SET key value [EX seconds]` | Set a key-value pair, optionally with TTL |
| `GET` | `GET key` | Retrieve value by key |
| `DEL` | `DEL key` | Delete a key |
| `EXPIRE` | `EXPIRE key seconds` | Set TTL on an existing key |
| `KEYS` | `KEYS pattern` | List keys matching a glob pattern |
| `PING` | `PING` | Health check, returns `PONG` |
| `RANGE` | `RANGE start end` | Range query over SkipList (lexicographic) |
---
## Testing
```bash
# Run all tests with race detection
go test -race -v ./...
# Run benchmarks
go test -bench=. -benchmem ./internal/engine/
go test -bench=. -benchmem ./internal/eviction/
# Run integration tests
go test -tags=integration -v ./tests/
# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```
---
## Design Decisions
| Decision | Choice | Rationale |
|---|---|---|
| Data structure | SkipList over B-Tree | Simpler implementation, natural lock-free concurrent reads, O(log N) range queries. Redis uses SkipLists for sorted sets. Trade-off: worse cache locality than B-Trees. |
| Concurrency model | Goroutine-per-connection | Go's netpoller uses epoll internally. We get event-loop performance with sequential programming model. ~2KB/goroutine = 10K connections in ~20MB. |
| WAL flush strategy | Background goroutine with batched fsync | Hot path isn't blocked by disk I/O. Trade-off: small window of data loss on hard crash (configurable). Same trade-off as PostgreSQL's `wal_writer_delay`. |
| Eviction architecture | Strategy Pattern (interface) | Engine holds `EvictionPolicy` interface. LRU and LFU are injected at startup. Open/Closed Principle: add new policies without modifying the engine. |
| Command execution | Command Pattern | Each command is a self-contained object. Enables WAL serialization, replay for crash recovery, and undo/redo extension. |
| Raft node states | State Pattern | Leader, Follower, Candidate implement the same interface. Node transitions between states cleanly without massive switch statements. |
| Inter-node RPC | gRPC over raw TCP | Protobuf schemas prevent serialization bugs. HTTP/2 multiplexing. Built-in deadlines. Trade-off: ~50μs overhead per RPC, acceptable for consensus (small payloads). |
For deep trade-off analysis, see [docs/trade-offs.md](docs/trade-offs.md).
---
## Documentation
- **[High-Level Design (HLD)](docs/hld.md)** — System architecture, data flow diagrams, network topology
- **[Low-Level Design (LLD)](docs/lld.md)** — Interface contracts, design patterns, class diagrams
- **[Trade-offs](docs/trade-offs.md)** — Deep analysis of every major design decision
---
## Roadmap
- [x] Phase 0: Documentation & architecture design
- [ ] Phase 1: Single-node KV store (TCP server, binary protocol, SkipList, eviction, TTL)
- [ ] Phase 2: Persistence (WAL, snapshots, crash recovery)
- [ ] Phase 3: Raft consensus (leader election, log replication, gRPC transport)
- [ ] Phase 4: Stretch goals (raw TCP inter-node, `sync.Pool`, pub/sub, cluster sharding)
---
## References
- [Redis Internals](https://redis.io/docs/reference/internals/) — Inspiration for the single-threaded event loop model
- [In Search of an Understandable Consensus Algorithm (Raft Paper)](https://raft.github.io/raft.pdf) — Ongaro & Ousterhout, 2014
- [Skip Lists: A Probabilistic Alternative to Balanced Trees](https://15721.courses.cs.cmu.edu/spring2018/papers/08-oltpindexes1/pugh-skiplists-cacm1990.pdf) — Pugh, 1990
- [Write-Ahead Logging (PostgreSQL docs)](https://www.postgresql.org/docs/current/wal-intro.html)
---
## License
MIT