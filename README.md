# Coupon Issuance System

## Architecture

### Core Components

- **Database**: PostgreSQL for persistent storage of campaign and coupon data
- **Redis Cache**: Manages campaign status and coupon issuing with atomic operations
- **Background Workers**:
  - Campaign Status Worker: Activates campaigns based on start time
  - Coupon Code Writer: Asynchronously writes issued codes to database in batch
- **API Layer**: Connect/gRPC interface for high-performance communication

### Key Features

- Atomic coupon issuance using Redis counters
- Asynchronous database writes for better performance
- Unique code generation with configurable format
- Campaign lifecycle management

### Performance Characteristics

- Supports 1000+ coupon issuances per second
- Handles multiple concurrent campaigns
- Maintains data consistency under load
- Low latency response times

### API

The system exposes a Connect/gRPC API with two main endpoints:

1. `CreateCampaign`: Creates new coupon campaigns with parameters like:
   - Campaign name
   - Start time
   - Coupon limit

2. `IssueCoupon`: Issues unique coupon codes for a campaign with:
   - Atomic counter verification
   - Unique code generation
   - Async database persistence

## Test

```sh
export POSTGRES_HOST=postgres
export REDIS_HOST=redis
docker compose down -v
docker compose up --build

./test.sh
```