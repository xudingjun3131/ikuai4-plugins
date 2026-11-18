# Personal Docker Compose

Single container: MITM proxy + control UI. Data in volume `proxying-data` (`/data` in container = CA, rules, scripts, collections).

## Run

```bash
cd deploy/compose
docker compose config    # validate
docker compose up -d --build
curl -s http://127.0.0.1:9090/api/health
```

UI: http://127.0.0.1:9090  
Proxy: `http://127.0.0.1:8080`

## Security

- Compose publishes **127.0.0.1 only**.
- Binding the host to `0.0.0.0` exposes capture + rules to the network — use only on trusted LAN with firewall.
- No sync/DB/team sidecars by design.

## Stop

```bash
docker compose down
# keep data:
# docker compose down   # volume retained
# wipe data:
# docker compose down -v
```
