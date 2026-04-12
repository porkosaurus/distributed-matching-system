# Distributed Matching Engine

A high-performance limit order book and matching engine built in Rust, 
with a distributed Go replication layer (in progress).

## Architecture

- **Rust core**: Lock-free order book using BTreeMap with price-time priority matching
- **Go gateway**: WebSocket ingress + sequencer (week 3)
- **Raft replication**: 3-node consensus layer (week 5)
- **React frontend**: Live L2 depth chart + trade tape (week 7)

## Performance

- 196ns median match latency (criterion benchmark, 100 samples)
- 11ns standard deviation — consistent, predictable hot path
- Lock-free ingress via crossbeam SegQueue — no mutex on matching path
- 40,000 orders processed across 4 concurrent producer threads

## Running

```bash
cargo run      # run the engine with 4 producer threads
cargo test     # run all 8 correctness tests
cargo bench    # run criterion benchmarks
```

## Progress

- [x] Week 1: Order book core (BTreeMap, price-time priority, fills)
- [x] Week 2: Lock-free concurrent ingress + criterion benchmarks
- [ ] Week 3: Go WebSocket gateway + sequencer
- [ ] Week 4: Risk checks + append-only event log
- [ ] Week 5: Raft consensus replication
- [ ] Week 6: Snapshots + chaos testing
- [ ] Week 7: React frontend
- [ ] Week 8: Prometheus + Grafana metrics
- [ ] Week 9: NASDAQ ITCH replay + market making bot
- [ ] Week 10: Docker Compose + polish