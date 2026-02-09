# WikiSurge Capacity Planning

## Overview

This document outlines resource requirements for WikiSurge at different scales,
cost estimates, scaling triggers, and growth projections.

---

## Resource Requirements by Scale

### Small: 1K edits/day (~0.01 edits/sec)

| Resource        | Requirement           |
|-----------------|-----------------------|
| **CPU**         | 1 vCPU                |
| **RAM**         | 2 GB                  |
| **Disk**        | 10 GB SSD             |
| **Instances**   | 1 (all-in-one)        |
| **Redis**       | 256 MB memory         |
| **Elasticsearch** | 512 MB heap         |
| **Kafka**       | Optional (direct SSE) |
| **Network**     | 1 Mbps                |

**Deployment Architecture:**
```
┌─────────────────────────────────────────┐
│           Single Instance               │
│  ┌───────┐ ┌──────────┐ ┌───────────┐  │
│  │Ingestor│ │Processor │ │    API    │  │
│  └───┬───┘ └────┬─────┘ └─────┬─────┘  │
│      │          │              │         │
│  ┌───┴──────────┴──────────────┴──────┐ │
│  │          Redis (256MB)             │ │
│  └────────────────────────────────────┘ │
│  ┌────────────────────────────────────┐ │
│  │     Elasticsearch (optional)       │ │
│  └────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

**Estimated Monthly Cost:**
- AWS: t3.small ($15-20/mo) + EBS ($1/mo) ≈ **$20/month**
- DigitalOcean: Basic Droplet ($12/mo) ≈ **$12/month**
- Self-hosted: Minimal (Raspberry Pi capable)

---

### Medium: 100K edits/day (~1.2 edits/sec)

| Resource          | Requirement            |
|-------------------|------------------------|
| **CPU**           | 2 vCPU per service     |
| **RAM**           | 4 GB per service       |
| **Disk**          | 50 GB SSD              |
| **Instances**     | 2-3 (service split)    |
| **Redis**         | 1 GB memory, dedicated |
| **Elasticsearch** | 2 GB heap, 1 node      |
| **Kafka**         | 1 broker, 3 partitions |
| **Network**       | 10 Mbps                |

**Deployment Architecture:**
```
┌───────────────┐  ┌────────────────────────┐
│   Instance 1  │  │      Instance 2        │
│ ┌───────────┐ │  │ ┌──────────┐ ┌───────┐ │
│ │ Ingestor  │ │  │ │Processor │ │  API  │ │
│ └─────┬─────┘ │  │ └────┬─────┘ └───┬───┘ │
└───────│───────┘  └──────│────────────│─────┘
        │                 │            │
   ┌────┴─────────────────┴────────────┴────┐
   │            Kafka (1 broker)            │
   └────────────────┬───────────────────────┘
                    │
   ┌────────────────┴───────────────────────┐
   │   Instance 3 (Data)                    │
   │  ┌─────────┐  ┌────────────────────┐   │
   │  │  Redis  │  │  Elasticsearch     │   │
   │  │ (1 GB)  │  │  (2 GB heap)       │   │
   │  └─────────┘  └────────────────────┘   │
   └────────────────────────────────────────┘
```

**Estimated Monthly Cost:**
- AWS: 2× t3.medium + 1× m5.large ($50+$50+$80) + EBS ≈ **$200/month**
- DigitalOcean: 2× Premium ($24 each) + 1× ($48) ≈ **$100/month**
- Managed services: Redis ($25) + ES ($50) + VMs ($80) ≈ **$155/month**

---

### Large: 1M edits/day (~12 edits/sec)

| Resource          | Requirement                |
|-------------------|----------------------------|
| **CPU**           | 4 vCPU per service         |
| **RAM**           | 8 GB per service           |
| **Disk**          | 200 GB SSD (provisioned IOPS) |
| **Instances**     | 5+ with load balancer      |
| **Redis**         | 4 GB, Redis Cluster or Sentinel |
| **Elasticsearch** | 4 GB heap, 3-node cluster  |
| **Kafka**         | 3 brokers, 6 partitions    |
| **Network**       | 100 Mbps                   |
| **Load Balancer** | Application LB (L7)       |

**Deployment Architecture:**
```
                    ┌─────────────┐
                    │Load Balancer│
                    └──────┬──────┘
              ┌────────────┼────────────┐
        ┌─────┴─────┐┌────┴─────┐┌─────┴─────┐
        │  API (1)  ││  API (2) ││  API (3)  │
        └─────┬─────┘└────┬─────┘└─────┬─────┘
              └────────────┼────────────┘
                    ┌──────┴──────┐
        ┌───────────┤  Kafka (3)  ├───────────┐
        │           └─────────────┘           │
   ┌────┴─────┐  ┌──────────┐  ┌─────────────┴──┐
   │Ingestor 1│  │Ingestor 2│  │ Processor (2-3) │
   └──────────┘  └──────────┘  └─────────────────┘
                       │
        ┌──────────────┼──────────────┐
   ┌────┴─────┐  ┌────┴─────┐  ┌────┴─────┐
   │ Redis M  │  │ Redis R1 │  │ Redis R2 │
   │(Primary) │  │(Replica) │  │(Replica) │
   └──────────┘  └──────────┘  └──────────┘
                       │
        ┌──────────────┼──────────────┐
   ┌────┴─────┐  ┌────┴─────┐  ┌────┴─────┐
   │  ES (1)  │  │  ES (2)  │  │  ES (3)  │
   └──────────┘  └──────────┘  └──────────┘
```

**Estimated Monthly Cost:**
- AWS: 5× m5.xlarge + managed Redis/ES + Kafka ≈ **$800-1200/month**
- DigitalOcean: 5× Premium ($96) + Managed DB ≈ **$600/month**
- Self-hosted (bare metal): Dedicated servers ≈ **$400-600/month**

---

## Cost Comparison Summary

| Scale       | Edits/Day | AWS        | DigitalOcean | Self-hosted |
|-------------|-----------|------------|--------------|-------------|
| **Small**   | 1K        | $20/mo     | $12/mo       | $10/mo      |
| **Medium**  | 100K      | $200/mo    | $100/mo      | $80/mo      |
| **Large**   | 1M        | $1,000/mo  | $600/mo      | $500/mo     |
| **X-Large** | 10M       | $5,000/mo  | $3,000/mo    | $2,000/mo   |

---

## Scaling Triggers

### Automatic Scaling Indicators

| Metric                     | Threshold            | Action                        |
|----------------------------|----------------------|-------------------------------|
| CPU utilization            | > 70% for 5 min      | Add instance                  |
| Memory utilization         | > 80%                | Add memory / instance         |
| API p99 latency            | > 200ms sustained    | Add API instances             |
| Kafka consumer lag         | > 10,000 messages    | Add processor instances       |
| Redis memory usage         | > 80% maxmemory      | Increase memory allocation    |
| ES JVM heap               | > 75%                | Add ES node                   |
| WebSocket connections      | > 80% max            | Add API instances             |
| Error rate                 | > 1% for 5 min       | Investigate + scale           |
| Disk usage                 | > 85%                | Expand / cleanup              |
| Request queue depth        | > 1000               | Add capacity                  |

### Manual Scaling Checklist

1. **Before scaling up:**
   - Profile to identify bottlenecks
   - Optimize before adding hardware
   - Check for memory leaks or goroutine leaks
   - Review database query efficiency

2. **When scaling horizontally:**
   - Ensure stateless services (API, ingestor)
   - Use sticky sessions for WebSocket (or shared pub/sub)
   - Add Kafka partitions before adding consumers
   - Consider Redis Cluster for data sharding

3. **When scaling vertically:**
   - Increase JVM heap for Elasticsearch (max 50% of RAM)
   - Increase Redis maxmemory
   - Increase Go GOMAXPROCS if CPU-bound

---

## Growth Projections

Based on Wikipedia's actual edit rate (~6-7 edits/second globally):

| Timeframe   | Projected Edits/Day | Scale       | Resources Needed   |
|-------------|---------------------|-------------|-------------------|
| Launch      | 1K-10K              | Small       | 1 instance        |
| 3 months    | 10K-50K             | Small-Medium| 1-2 instances     |
| 6 months    | 50K-200K            | Medium      | 2-3 instances     |
| 1 year      | 200K-500K           | Medium-Large| 3-5 instances     |
| 2 years     | 500K-1M             | Large       | 5+ instances      |

### Storage Growth

| Component       | Growth Rate      | 6 Months  | 1 Year    | 2 Years   |
|-----------------|------------------|-----------|-----------|-----------|
| Redis (hot data)| ~1 MB/day        | 180 MB    | 365 MB    | 730 MB    |
| Elasticsearch   | ~50 MB/day (7d)  | 350 MB    | 350 MB*   | 350 MB*   |
| Kafka logs      | ~100 MB/day (3d) | 300 MB    | 300 MB*   | 300 MB*   |
| Metrics (Prom)  | ~10 MB/day (30d) | 300 MB    | 300 MB*   | 300 MB*   |

*With retention policies, storage is bounded.

---

## Performance Baselines

| Metric                    | Target       | Measured (baseline) |
|---------------------------|-------------|---------------------|
| API p50 latency           | < 25ms      | TBD (run benchmarks)|
| API p99 latency           | < 100ms     | TBD                 |
| WebSocket message latency | < 50ms      | TBD                 |
| Kafka produce latency     | < 10ms      | TBD                 |
| Kafka consume lag         | < 100 msgs  | TBD                 |
| Redis GET latency         | < 1ms       | TBD                 |
| ES search latency         | < 100ms     | TBD                 |
| Memory per WS connection  | < 50KB      | TBD                 |
| Max sustained edit rate   | 50 edits/s  | TBD                 |

---

## Disaster Recovery

| Scenario              | RPO      | RTO      | Strategy                    |
|-----------------------|----------|----------|-----------------------------|
| Redis failure         | 0 (AOF)  | < 30s    | Sentinel auto-failover      |
| ES node failure       | ~1s      | < 60s    | Replica promotion           |
| Kafka broker failure  | 0        | < 30s    | ISR replication             |
| Full region failure   | < 1 min  | < 15 min | Cross-region replication    |
| Data corruption       | Last backup| < 1hr  | Point-in-time restoration   |

---

## Recommendations

1. **Start small** — single instance handles up to 10K edits/day easily
2. **Monitor first** — establish baselines before scaling decisions
3. **Optimize before scaling** — profiling often reveals 2-5x improvements
4. **Use managed services** when budget allows — reduces ops burden
5. **Plan for 2x headroom** — scale before hitting limits
6. **Automate scaling triggers** — use Prometheus alerts + auto-scaling groups
7. **Regular load testing** — run monthly to catch regressions
