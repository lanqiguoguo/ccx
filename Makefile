# CCX Makefile

GREEN=\033[0;32m
YELLOW=\033[0;33m
NC=\033[0m

.PHONY: help install dev run build clean frontend-dev frontend-build embed-frontend 

help:
	@echo "$(GREEN)CCX - 可用命令:$(NC)"
	@echo ""
	@echo "$(YELLOW)环境准备:$(NC)"
	@echo "  make install        - 安装所有依赖（前端 + 后端）"
	@echo ""
	@echo "$(YELLOW)开发:$(NC)"
	@echo "  make dev            - Go 后端热重载开发(不含前端)"
	@echo "  make run            - 构建前端并运行 Go 后端"
	@echo "  make frontend-dev   - 前端开发服务器"
	@echo ""
	@echo "$(YELLOW)构建:$(NC)"
	@echo "  make build          - 构建前端并编译 Go 后端"
	@echo "  make frontend-build - 仅构建前端"
	@echo "  make clean          - 清理构建文件"

install:
	@echo "$(GREEN)📦 安装前端依赖...$(NC)"
	@cd frontend && bun install
	@echo "$(GREEN)📦 安装桌面前端依赖...$(NC)"
	@echo "$(GREEN)📦 下载 Go 后端依赖...$(NC)"
	@cd backend-go && go mod download
	@echo "$(GREEN)📦 下载桌面端 Go 依赖...$(NC)"
	@echo "$(GREEN)📦 安装 Wails 3 CLI...$(NC)"
	@if ! command -v air &> /dev/null; then \
		echo "$(GREEN)📦 安装 Air 热重载工具...$(NC)"; \
		go install github.com/air-verse/air@latest; \
	else \
		echo "$(GREEN)✅ Air 已安装，跳过$(NC)"; \
	fi
	@echo "$(GREEN)✅ 所有依赖安装完成$(NC)"

dev:
	@echo "$(GREEN)🚀 启动前后端开发模式...$(NC)"
	@cd frontend && bun run dev &
	@cd backend-go && $(MAKE) dev

run: embed-frontend
	@cd backend-go && $(MAKE) run

build: embed-frontend
	@cd backend-go && $(MAKE) build



FRONTEND_SENTINEL=backend-go/frontend/dist/.build-sentinel
FRONTEND_SOURCES=$(shell find frontend/src frontend/public frontend/index.html frontend/vite.config.ts frontend/tsconfig.json frontend/tsconfig.app.json -type f 2>/dev/null)

embed-frontend: $(FRONTEND_SENTINEL)

$(FRONTEND_SENTINEL): $(FRONTEND_SOURCES)
	@bash scripts/embed-frontend.sh
	@touch $(FRONTEND_SENTINEL)

clean:
	@cd backend-go && $(MAKE) clean
	@rm -rf frontend/dist
	

frontend-dev:
	@cd frontend && bun run dev

frontend-build:
	@cd frontend && bun run build
