CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

.PHONY: dev build

# 开发模式：自动编译+运行，dashboard 从磁盘热加载，内置测试 token
dev:
	AICHATLOG_DEV=1 CGO_CFLAGS="$(CGO_CFLAGS)" go run ./cmd/server

# 编译二进制
build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -o aichatlog-server ./cmd/server
