# DNSLogger

自建DNSLog平台

### 编译 & 运行

```
export GOPROXY=https://proxy.golang.com.cn,direct # 可选
go build
./dnslogger

因采用了go-sqlite3组件，涉及到CGO，编译有问题请参考 https://github.com/mattn/go-sqlite3
```

### 配置

```
准备一个域名，假设为dnslogger.local
准备一个公网服务器，假设服务器IP为1.1.1.1
采用*.log.dnslogger.local为DNSLog接收子域，例如：miao.log.dnslogger.local
第一步，添加NS记录 log -> ns.dnslogger.local.
第二步，添加A记录 ns -> 1.1.1.1
第三步，在服务器上运行dnslogger
```

### 测试

```
# 发送DNS请求
dig dnslogger.local @1.1.1.1

# 查询最新的5条DNS请求
curl http://localhost:2020/api/latest -v

# 查询domain为dnslogger.local的请求（5分钟内）
curl http://localhost:2020/api/validate -d '{"domain":"dnslogger.local"}' -v
```

### 说明 & API

API端口默认为2020，可在配置文件中`listen_http`修改

API：

```
查看最新5条记录
GET /api/latest

根据域名查询DNS请求
POST /api/validate

参数：
{"domain":"dnslogger.local"}
```

### 常见问题

```
1. Ubuntu无法监听UDP53端口
Ubuntu默认安装了systemd-resolved服务，会监听UDP53端口，
导致DNSLogger无法监听UDP53端口，需要关闭systemd-resolved服务。

systemctl stop systemd-resolved.service # 停止服务
systemctl disable systemd-resolved.service # 禁止开机启动
echo "nameserver 223.5.5.5" > /etc/resolv.conf # 设置DNS服务器

2. UDP53端口权限问题
通常情况下，非root用户无法监听UDP53端口，需要给予CAP_NET_BIND_SERVICE权限。
（我也不知道怎么搞，因为上面这句话是Github Copilot生成的）
建议直接加sudo运行dnslogger
```
