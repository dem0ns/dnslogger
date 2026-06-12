# DNSLogger

自建 DNSLog 平台，用于安全测试中检测 DNS 请求回连。

## 功能特性

- DNS 请求记录与实时流（WebSocket）
- A 记录规则：通配符 / 正则 / 精确 / 匹配，每条可配独立 IP
- 上游 DNS 转发，无规则时默认放行
- Web 管理界面：日志查询、配置管理、规则管理
- 多语言支持（中文 / English）
- 简单模式：`dnslogger simple`，无需数据库

## 快速开始

```bash
go build -o dnslogger .
./dnslogger
```

打开 `http://127.0.0.1:8053` 访问管理界面。

## 简单模式

```bash
./dnslogger simple --addr :53 --ip 12.12.12.12
```

## License

[MIT](LICENSE)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=dem0ns/dnslogger&type=Date)](https://star-history.com/#dem0ns/dnslogger&Date)
