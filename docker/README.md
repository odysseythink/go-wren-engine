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
| `etc/config.properties` parsed at startup | ✅ Phase 2 |
| `PATCH /v1/config` persists to disk with archive | ✅ Phase 2 |
| Postgres wire protocol (port 7432) not implemented | PG clients cannot connect | TBD |
| Dynamic fields (`enable-dynamic-fields=true`) uses static branch | Possible semantic deviation | Phase 5 |

> **Note on PATCH persistence:** Java `Properties.store()` overwrites the entire
> file, discarding comments and custom headers. Go mirrors this behavior.
> If you rely on comments in `config.properties`, manage the file with git
> or an external templating tool.

## Makefile helpers

- `make image` — build the Docker image
- `make image-run` — build and run a container in foreground
- `make image-test` — build, run, and run a smoke test via HTTP
- `make image-clean` — remove the local image
