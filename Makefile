# 定义编译器和编译选项
GO := go
GOCMD := $(GO)
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
BINARY_NAME := manifestGenerator
SRCS := main.go

# 默认目标：构建主程序
build:
	$(GOBUILD) -o $(BINARY_NAME) $(SRCS)

# 清理生成的二进制文件
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

# 运行应用
run:
	$(GOBUILD) -o $(BINARY_NAME) $(SRCS)
	./$(BINARY_NAME)

# 测试
test:
	$(GOTEST) -v ./...

# 获取项目依赖
get:
	$(GOGET)

# 默认任务，构建主程序
default: build