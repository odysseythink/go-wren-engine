# go-wren-engine Docker Image

Drop-in replacement for the Java `wren-engine` image in WrenAI.

## Replace WrenAI compose image (3 steps)

1. Build the image locally:
   ```bash
   docker build -t go-wren-engine:latest -f docker/Dockerfile .
   ```

2. In your WrenAI `docker/docker-compose.yaml`, change the `wren-engine` service:
   ```yaml
   wren-engine:
     image: go-wren-engine:latest
     # keep all other fields (ports, volumes, env) unchanged
   ```

3. Start WrenAI:
   ```bash
   docker compose up -d
   ```

The go-wren-engine image uses the same mount path (`/usr/src/app/etc`),
accepts the same heap-size environment variables (`MAX_HEAP_SIZE`, `MIN_HEAP_SIZE`),
and listens on the same HTTP port (`8080`).

## Known drop-in gaps

| Gap | Impact | ETA |
|---|---|---|
| `etc/config.properties` is not parsed (env vars only) | Config file ignored | Phase 2 |
| Postgres wire protocol (port 7432) not implemented | PG clients cannot connect | TBD |
| `PATCH /v1/config` does not persist to disk | Restart loses config changes | Phase 2 |
| Dynamic fields (`enable-dynamic-fields=true`) uses static branch | Possible semantic deviation | Phase 5 |

## Makefile helpers

- `make image` — build the Docker image
- `make image-run` — build and run a container in foreground
- `make image-test` — build, run, and run a smoke test via HTTP
- `make image-clean` — remove the local image
