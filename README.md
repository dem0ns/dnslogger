# DNSLogger

### 适用场景

自建DNSLog环境。

### 说明

初次使用请将`config.default.ini`文件重命名为`config.ini`，并填写数据库连接信息。

### 编译

`go build`

### 在Docker中运行

`CGO_ENABLED=0 GOOS=linux go build && docker-compose up -d`
