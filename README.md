# DNSLogger

### 适用场景

自建DNSLog

![Running](./images/running.jpg)

### 说明

udp 53 DNS

tcp 1965 API

API：

查看最新5条记录 GET `/api/latest`

根据域名查询 POST `/api/validate` `{"Domain":"dnslogger.local"}`

### 测试

`dig dnslogger.local @127.0.0.1`

`curl http://localhost:1965/api/latest -v`

`curl http://localhost:1965/api/validate -d '{"domain":"dnslogger.local"}' -v`

### 编译

因采用了go-sqlite3组件，涉及到CGO，编译有问题请参考 https://github.com/mattn/go-sqlite3

### 在Docker中运行

`CGO_ENABLED=1 GOOS=linux go build && docker-compose up -d`
