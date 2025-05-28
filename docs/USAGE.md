# AI 代码审查机器人使用指南

本文档详细介绍了AI代码审查机器人的两种主要使用方式：AWS Lambda和GitHub Action。

## 目录
- [AWS Lambda 部署与使用](#aws-lambda-部署与使用)
  - [准备工作](#lambda-准备工作)
  - [构建与部署](#lambda-构建与部署)
  - [配置](#lambda-配置)
  - [使用流程](#lambda-使用流程)
  - [故障排除](#lambda-故障排除)
- [GitHub Action 部署与使用](#github-action-部署与使用)
  - [准备工作](#github-action-准备工作)
  - [配置工作流](#github-action-配置工作流)
  - [高级配置](#github-action-高级配置)
  - [使用流程](#github-action-使用流程)
  - [故障排除](#github-action-故障排除)
- [配置参数对照表](#配置参数对照表)

## AWS Lambda 部署与使用

AWS Lambda允许您在不管理服务器的情况下运行代码。这种部署方式适合需要独立于GitHub之外运行的场景，或者需要自定义处理逻辑的场景。

### Lambda 准备工作

1. **AWS账户**：确保您有一个有效的AWS账户，并具有创建Lambda函数和API Gateway的权限
2. **IAM角色**：创建一个具有以下权限的IAM角色：
   - `AWSLambdaBasicExecutionRole`（用于日志记录）
   - 如果需要访问其他AWS服务，请添加相应的权限
3. **API密钥**：准备好以下API密钥：
   - GitHub访问令牌（需要`repo`和`pull_request`权限）
   - OpenAI API密钥或其他LLM服务的API密钥

### Lambda 构建与部署

#### 方法1：手动构建与部署

1. **构建二进制文件**：
   ```bash
   # 为Lambda环境编译（Linux环境）
   GOOS=linux GOARCH=amd64 go build -o bootstrap ./cmd/lambda
   
   # 打包为ZIP文件
   zip lambda.zip bootstrap
   ```

2. **创建Lambda函数**：
   - 登录AWS管理控制台，导航到Lambda服务
   - 点击"创建函数"
   - 选择"从头开始创作"
   - 填写函数名称，如`ai-code-reviewer`
   - 运行时选择"提供自己的引导程序"
   - 架构选择"x86_64"
   - 选择或创建执行角色
   - 点击"创建函数"

3. **上传代码**：
   - 在函数页面，选择"上传自"→"ZIP文件"
   - 上传之前创建的`lambda.zip`文件
   - 处理程序保持为`bootstrap`

4. **配置API Gateway**：
   - 在Lambda函数页面，添加API Gateway触发器
   - 创建新的REST API或HTTP API
   - 安全性选择"开放"（稍后通过Webhook密钥保护）
   - 部署阶段选择"prod"或创建新的阶段
   - 记录生成的API端点URL

#### 方法2：使用AWS SAM（Serverless Application Model）

1. **创建SAM模板**：
   创建`template.yaml`文件：
   ```yaml
   AWSTemplateFormatVersion: '2010-09-09'
   Transform: AWS::Serverless-2016-10-31
   Resources:
     AICodeReviewerFunction:
       Type: AWS::Serverless::Function
       Properties:
         CodeUri: ./
         Handler: bootstrap
         Runtime: provided.al2
         Architectures:
           - x86_64
         Events:
           ApiEvent:
             Type: Api
             Properties:
               Path: /webhook
               Method: post
         Environment:
           Variables:
             GITHUB_TOKEN: your-github-token
             OPENAI_API_KEY: your-openai-api-key
             LOG_LEVEL: info
             LANGUAGE: chinese
   ```

2. **构建与部署**：
   ```bash
   # 构建Lambda函数
   GOOS=linux GOARCH=amd64 go build -o bootstrap ./cmd/lambda
   
   # 使用SAM部署
   sam deploy --guided
   ```

### Lambda 配置

在Lambda函数配置中，设置以下环境变量：

| 环境变量 | 描述 | 示例值 |
|---------|------|--------|
| `GITHUB_TOKEN` | GitHub访问令牌 | `ghp_xxxxxxxxxxxx` |
| `OPENAI_API_KEY` | OpenAI API密钥 | `sk-xxxxxxxxxxxx` |
| `LOG_LEVEL` | 日志级别（可选） | `info`、`debug`、`warn`、`error` |
| `LANGUAGE` | 评论语言（可选） | `chinese`或`english` |
| `WEBHOOK_SECRET` | Webhook密钥（可选但推荐） | `your-secure-secret` |
| `INCLUDE_PATTERNS` | 包含的文件模式（可选） | `*.go,*.js,*.py` |
| `IGNORE_PATTERNS` | 忽略的文件模式（可选） | `vendor/*,node_modules/*` |
| `MAX_PATCH_LENGTH` | 最大补丁长度（可选） | `50000` |

### Lambda 使用流程

1. **配置GitHub Webhook**：
   - 在GitHub仓库中，导航到"Settings" → "Webhooks" → "Add webhook"
   - 填写Payload URL（使用API Gateway生成的URL）
   - 内容类型选择`application/json`
   - 如果设置了Webhook密钥，填入相同的密钥
   - 选择"Let me select individual events"，然后选择"Pull requests"
   - 点击"Add webhook"

2. **测试Webhook**：
   - 创建一个测试PR
   - 检查Lambda函数的CloudWatch日志，确认是否收到Webhook请求
   - 验证PR上是否出现代码审查评论

3. **监控与日志**：
   - 使用CloudWatch监控Lambda函数的执行情况
   - 设置警报以便在函数失败时收到通知

### Lambda 故障排除

- **函数超时**：默认Lambda超时为3秒，对于大型代码审查可能不够。建议将超时设置为至少30秒。
- **内存限制**：如果处理大型PR，可能需要增加内存分配（建议至少512MB）。
- **权限问题**：确保Lambda函数的执行角色有足够的权限。
- **Webhook验证失败**：检查Webhook密钥是否正确设置。
- **API限流**：如果遇到LLM API限流，考虑实现重试逻辑或降低并发请求数。

## GitHub Action 部署与使用

GitHub Action提供了更简单的集成方式，直接在GitHub工作流中运行代码审查。这种方式适合已经使用GitHub Actions的项目，配置更简单。

### GitHub Action 准备工作

1. **GitHub仓库**：确保您有权限配置GitHub Actions的仓库
2. **API密钥**：准备好以下API密钥：
   - GitHub Token（通常使用自动提供的`GITHUB_TOKEN`）
   - OpenAI API密钥或其他LLM服务的API密钥

### GitHub Action 配置工作流

1. **创建工作流文件**：
   在仓库中创建`.github/workflows/code-review.yml`文件：

   ```yaml
   name: AI Code Review

   on:
     pull_request:
       types: [opened, synchronize]

   jobs:
     review:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v3
           with:
             fetch-depth: 0
         
         - name: AI Code Review
           uses: eust-w/ai_code_reviewer@main
           with:
             github_token: ${{ secrets.GITHUB_TOKEN }}
             openai_api_key: ${{ secrets.OPENAI_API_KEY }}
             language: chinese
   ```

2. **配置Secrets**：
   - 在GitHub仓库中，导航到"Settings" → "Secrets and variables" → "Actions"
   - 添加名为`OPENAI_API_KEY`的secret，值为您的OpenAI API密钥
   - 其他LLM服务的API密钥也可以类似方式添加

### GitHub Action 高级配置

GitHub Action支持以下输入参数：

| 参数 | 描述 | 默认值 |
|-----|------|-------|
| `github_token` | GitHub令牌 | `${{ secrets.GITHUB_TOKEN }}` |
| `openai_api_key` | OpenAI API密钥 | - |
| `language` | 评论语言 | `chinese` |
| `include_patterns` | 包含的文件模式 | `*` |
| `ignore_patterns` | 忽略的文件模式 | - |
| `target_label` | 目标标签（只审查带有此标签的PR） | - |
| `max_patch_length` | 最大补丁长度 | `50000` |
| `model` | OpenAI模型名称 | `gpt-4o` |

高级配置示例：

```yaml
- name: AI Code Review
  uses: eust-w/ai_code_reviewer@main
  with:
    github_token: ${{ secrets.GITHUB_TOKEN }}
    openai_api_key: ${{ secrets.OPENAI_API_KEY }}
    language: english
    include_patterns: "*.go,*.js,*.py"
    ignore_patterns: "vendor/*,node_modules/*,*.generated.go"
    target_label: "needs-review"
    max_patch_length: 100000
    model: "gpt-4o"
```

### GitHub Action 使用流程

1. **提交工作流文件**：
   - 将配置好的工作流文件提交到仓库
   - 确保工作流文件位于`.github/workflows/`目录中

2. **创建PR**：
   - 创建一个测试PR
   - 观察GitHub Actions标签页，确认工作流是否正常运行
   - 验证PR上是否出现代码审查评论

3. **查看运行日志**：
   - 在GitHub Actions标签页中查看工作流运行情况
   - 如果遇到问题，检查日志以了解详细信息

### GitHub Action 故障排除

- **权限问题**：确保工作流有足够的权限创建PR评论。可能需要在工作流中添加：
  ```yaml
  permissions:
    pull-requests: write
    contents: read
  ```
- **超时问题**：如果代码审查时间过长，可能会超出GitHub Actions的默认超时时间。可以增加超时设置：
  ```yaml
  jobs:
    review:
      runs-on: ubuntu-latest
      timeout-minutes: 30
  ```
- **API限流**：如果遇到LLM API限流，考虑在工作流中添加重试逻辑。
- **大型PR**：对于非常大的PR，可能需要调整`max_patch_length`参数或考虑分割PR。

## 配置参数对照表

下表对比了Lambda和GitHub Action两种部署方式的配置参数：

| 功能 | Lambda环境变量 | GitHub Action输入 |
|-----|--------------|-----------------|
| GitHub令牌 | `GITHUB_TOKEN` | `github_token` |
| OpenAI API密钥 | `OPENAI_API_KEY` | `openai_api_key` |
| 评论语言 | `LANGUAGE` | `language` |
| 包含的文件模式 | `INCLUDE_PATTERNS` | `include_patterns` |
| 忽略的文件模式 | `IGNORE_PATTERNS` | `ignore_patterns` |
| 目标标签 | `TARGET_LABEL` | `target_label` |
| 最大补丁长度 | `MAX_PATCH_LENGTH` | `max_patch_length` |
| OpenAI模型 | `MODEL` | `model` |
| 日志级别 | `LOG_LEVEL` | 不适用（使用Actions日志） |
| Webhook密钥 | `WEBHOOK_SECRET` | 不适用（由GitHub管理） |

### 代码索引相关参数

代码索引功能可以显著提高代码审查的质量，通过分析代码库上下文来增强审查结果。

| 功能 | Lambda环境变量 | GitHub Action输入 |
|-----|--------------|------------------|
| 启用代码索引 | `ENABLE_INDEXING` | `enable_indexing` |
| 索引存储类型 | `INDEXER_STORAGE_TYPE` | `indexer_storage_type` |
| Chroma主机 | `INDEXER_CHROMA_HOST` | `indexer_chroma_host` |
| Chroma端口 | `INDEXER_CHROMA_PORT` | `indexer_chroma_port` |
| Chroma路径 | `INDEXER_CHROMA_PATH` | `indexer_chroma_path` |
| Chroma SSL | `INDEXER_CHROMA_SSL` | `indexer_chroma_ssl` |
| 本地存储路径 | `INDEXER_LOCAL_STORAGE_PATH` | `indexer_local_storage_path` |
| 向量服务类型 | `INDEXER_VECTOR_TYPE` | `indexer_vector_type` |

### 代码索引配置示例

#### 使用Chroma存储索引数据

[Chroma](https://www.trychroma.com/) 是一个开源的向量数据库，非常适合存储和检索代码片段的向量表示。下面是使用Chroma进行代码索引的配置示例：

```env
# 启用代码索引
# 设置为true来启用代码索引功能
# 这将显著提高代码审查的质量，但会增加资源消耗
# 默认值：false
ENABLE_INDEXING=true

# 索引存储类型
# 可选值："chroma", "local"
# chroma: 使用Chroma向量数据库存储索引
# local: 使用本地文件系统存储索引
# 默认值：local
INDEXER_STORAGE_TYPE=chroma

# Chroma服务器配置
# 当INDEXER_STORAGE_TYPE=chroma时使用
INDEXER_CHROMA_HOST=localhost
INDEXER_CHROMA_PORT=8000
INDEXER_CHROMA_PATH=/api/v1
INDEXER_CHROMA_SSL=false

# 向量服务类型
# 可选值："openai", "local", "simple"
# openai: 使用OpenAI API生成向量表示
# local: 使用本地模型生成向量表示
# simple: 使用简单的基于规则的向量生成（不需要外部API）
# 默认值：simple
INDEXER_VECTOR_TYPE=openai

# 如果使用OpenAI生成向量，需要提供API密钥
# 当INDEXER_VECTOR_TYPE=openai时使用
OPENAI_API_KEY=sk-your-openai-api-key
```

#### 使用本地存储索引数据

如果您不想使用Chroma，可以选择使用本地文件系统存储索引数据：

```env
ENABLE_INDEXING=true
INDEXER_STORAGE_TYPE=local
INDEXER_LOCAL_STORAGE_PATH=./data/index
INDEXER_VECTOR_TYPE=simple
```

### 在GitHub Action中使用代码索引

要在GitHub Action中启用代码索引，可以添加以下配置：

```yaml
- name: AI Code Review with Indexing
  uses: eust-w/ai_code_reviewer@main
  with:
    github_token: ${{ secrets.GITHUB_TOKEN }}
    openai_api_key: ${{ secrets.OPENAI_API_KEY }}
    language: english
    enable_indexing: true
    indexer_storage_type: local
    indexer_local_storage_path: ./data/index
    indexer_vector_type: simple
```

### 代码索引工作原理

启用代码索引后，系统将执行以下操作：

1. **索引代码库**：在首次运行时，系统会分析并索引整个代码库
2. **提取上下文**：在审查PR时，系统会查询与变更文件相关的代码上下文
3. **增强补丁**：将相关的代码上下文添加到补丁中，增强审查的质量
4. **生成更准确的审查结果**：利用上下文信息，LLM可以生成更准确、更相关的审查结果

代码索引功能特别适用于大型代码库和复杂的PR，可以显著提高审查质量。

## 总结

- **Lambda部署**适合需要独立于GitHub之外运行的场景，或者需要自定义处理逻辑的场景。
- **GitHub Action部署**适合直接集成到GitHub CI/CD流程中的场景，配置更简单。

两种方式功能上基本相同，都是用来自动进行代码审查，选择哪种方式主要取决于您的基础设施和集成需求。
