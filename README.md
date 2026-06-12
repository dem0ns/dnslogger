# DNSLogger

A self-hosted DNSLog platform for security testing with a web dashboard.

![Running](images/running.png)

## Quick Start

```bash
go build
./dnslogger
```

Open `http://localhost:2020` to access the dashboard.

## Features

- DNS request logging
- Real-time log streaming via WebSocket
- Filter rules (allow/block) with pattern matching
- Configurable via web UI
- Simple mode for quick domain interception

## Configuration

```ini
[config]
db_file = dnslog.db
return_ip = 127.0.0.1
listen_dns = 0.0.0.0:53
listen_http = 0.0.0.0:2020
```

## Simple Mode

```bash
./dnslogger simple -addr :53 -ip 12.12.12.12
```

## License

[MIT](LICENSE)
