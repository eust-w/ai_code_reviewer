# AI 代码审查机器人

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go Version](https://img.shields.io/badge/go-1.18+-blue.svg)

AI 代码审查机器人是一个自动化工具，利用大型语言模型（LLM）对代码变更进行智能审查，提供高质量的代码反馈和建议。支持 GitHub、GitLab 和 Gitea 平台，可与多种 LLM 模型集成，包括 OpenAI、Claude 和 Deepseek。

## 功能特点

- **多平台支持**：兼容 GitHub、GitLab 和 Gitea 三大代码托管平台
- **多模型支持**：支持多种 LLM 模型，包括：
  - OpenAI (GPT-4, GPT-4o, GPT-3.5-Turbo)
  - Azure OpenAI
  - Claude (Claude 3.5 Sonnet)
  - Deepseek (Deepseek V3)
  - 通过代理支持其他模型
- **智能代码审查**：分析代码变更，识别潜在问题、风险和改进机会
- **结构化反馈**：提供清晰的审查结果，包括：
  - 代码变更总结
  - 改进建议
  - 代码亮点
  - 潜在风险
- **大型补丁处理**：能够处理大型代码补丁，通过分割和合并策略避免超出 LLM 的 token 限制
- **文件过滤**：支持通过 glob 模式匹配来包含或排除特定文件
- **多语言支持**：支持中英文两种语言的代码审查结果
- **高度可配置**：通过环境变量提供丰富的配置选项

## 架构

AI 代码审查机器人由以下主要组件组成：

1. **Git 平台接口**：处理与代码托管平台的交互，包括获取 PR/MR 信息、比较提交和创建评论
2. **聊天模块**：负责与 LLM API 的交互，包括生成提示词、调用 API 和解析响应
3. **机器人核心**：协调整个审查流程，从获取代码变更到提交审查结果
4. **配置管理**：处理各种配置选项，支持灵活的部署场景

## 快速开始

### 前提条件

- Go 1.18 或更高版本（仅用于构建）
- Git 平台（GitHub、GitLab 或 Gitea）的访问权限
- LLM API 访问权限（OpenAI、Claude 或 Deepseek）

### 安装

```bash
# 克隆仓库
git clone https://github.com/eust-w/ai_code_reviewer.git
cd ai_code_reviewer

# 编译
make build
```

### 配置

创建 `.env` 文件，根据您的需求进行配置：

```env
# 平台选择: github, gitlab, gitea
PLATFORM=github

# GitHub 配置
GITHUB_TOKEN=your_github_token

# LLM 配置 (选择一种)
# 方式 1: LLM 代理配置
LLM_PROXY_ENDPOINT=https://your-llm-proxy-endpoint/v1/chat/completions
LLM_PROXY_API_KEY=your-llm-proxy-api-key
CLAUDE_MODEL_NAME=aws/claude-3-5-sonnet

# 方式 2: OpenAI 配置
# OPENAI_API_KEY=your-openai-api-key
# MODEL=gpt-4o

# 其他配置
LANGUAGE=Chinese  # 或 English
WEBHOOK_SECRET=your-secure-webhook-secret
```

查看 [DEPLOYMENT.md](./DEPLOYMENT.md) 获取完整的配置选项和部署指南。

### 运行

```bash
# 直接运行
./bin/cr-bot

# 或使用 Docker
docker run -p 8008:8008 --env-file .env ai-code-reviewer:latest
```

## 使用方式

本项目支持两种主要的使用方式：

1. **AWS Lambda 部署**：作为无服务器函数运行，通过API Gateway接收GitHub webhook请求
2. **GitHub Action 部署**：作为GitHub Actions工作流的一部分直接在PR中运行

详细的使用说明、配置参数和故障排除指南，请参考 [USAGE.md](./docs/USAGE.md)。

## 使用示例

### GitHub 工作流程

1. 配置 Webhook 和 GitHub Token
2. 开发者创建 Pull Request
3. AI 代码审查机器人自动审查代码变更
4. 机器人在 PR 上提交审查评论，包括：
   - 代码变更总结
   - 改进建议
   - 代码亮点
   - 潜在风险
5. 开发者根据反馈改进代码
6. 审查者参考 AI 反馈进行人工审查

### 示例输出

```markdown
**LGTM: ✅ 代码看起来不错**

## 代码变更总结
此 PR 实现了用户认证功能，添加了登录和注册 API 端点，以及 JWT 令牌验证中间件。

## 改进建议
- 考虑为密码哈希添加盐值以增强安全性
- 登录失败后应该有速率限制防止暴力攻击
- 令牌过期时间可以设置为环境变量而非硬编码

## 代码亮点
- 良好的错误处理和用户友好的错误消息
- 清晰的代码结构和函数命名
- 使用了参数化查询防止 SQL 注入

## 潜在风险
- 未见到对输入数据的充分验证，可能导致安全问题
- 缺少对数据库连接失败的优雅处理
```

## 部署

详细的部署指南请参考 [DEPLOYMENT.md](./DEPLOYMENT.md)，其中包含：

- 各平台（GitHub、GitLab、Gitea）的配置步骤
- 服务器部署方法（直接部署和 Docker 部署）
- Webhook 配置指南
- 故障排除提示

## 贡献

欢迎贡献代码、报告问题或提出改进建议！请遵循以下步骤：

1. Fork 仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 许可证

本项目采用 MIT 许可证 - 详情请参阅 [LICENSE](LICENSE) 文件。

## 致谢

- 感谢所有开源项目和库的贡献者
- 特别感谢 OpenAI、Anthropic 和 Deepseek 提供的 LLM 技术支持
