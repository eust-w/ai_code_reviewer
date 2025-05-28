package indexer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// TempRepoDir 返回临时仓库目录
func TempRepoDir(owner, repo string) string {
	// 创建一个安全的目录名（替换可能在文件路径中不安全的字符）
	safeOwner := strings.ReplaceAll(owner, "/", "_")
	safeRepo := strings.ReplaceAll(repo, "/", "_")
	
	// 使用系统临时目录作为基础
	baseDir := os.TempDir()
	return filepath.Join(baseDir, "ai_code_reviewer", "repos", safeOwner, safeRepo)
}

// CloneOrUpdateRepo 克隆或更新仓库
// 返回本地仓库路径和是否成功
func CloneOrUpdateRepo(platform, owner, repo, ref string, credentials map[string]string) (string, error) {
	repoDir := TempRepoDir(owner, repo)
	
	// 检查目录是否已存在
	_, err := os.Stat(repoDir)
	repoExists := !os.IsNotExist(err)
	
	// 构建克隆URL
	cloneURL, err := buildCloneURL(platform, owner, repo, credentials)
	if err != nil {
		return "", fmt.Errorf("failed to build clone URL: %w", err)
	}
	
	if repoExists {
		// 如果仓库已存在，执行fetch和checkout
		logrus.Infof("Repository already exists at %s, updating...", repoDir)
		
		// 切换到仓库目录
		cmd := exec.Command("git", "fetch", "origin")
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to fetch repository: %w", err)
		}
		
		// 检出指定的引用（分支或提交）
		cmd = exec.Command("git", "checkout", ref)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to checkout ref %s: %w", ref, err)
		}
		
		// 拉取最新代码
		cmd = exec.Command("git", "pull", "origin", ref)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			logrus.Warnf("Failed to pull latest changes: %v - continuing with existing code", err)
		}
	} else {
		// 如果仓库不存在，执行克隆
		logrus.Infof("Cloning repository %s/%s to %s...", owner, repo, repoDir)
		
		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(repoDir), 0755); err != nil {
			return "", fmt.Errorf("failed to create parent directory: %w", err)
		}
		
		// 克隆仓库
		cmd := exec.Command("git", "clone", cloneURL, repoDir)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to clone repository: %w", err)
		}
		
		// 检出指定的引用（如果不是默认分支）
		cmd = exec.Command("git", "checkout", ref)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to checkout ref %s: %w", ref, err)
		}
	}
	
	logrus.Infof("Repository ready at %s", repoDir)
	return repoDir, nil
}

// buildCloneURL 根据平台构建克隆URL
func buildCloneURL(platform, owner, repo string, credentials map[string]string) (string, error) {
	var cloneURL string
	
	switch strings.ToLower(platform) {
	case "github":
		token, ok := credentials["github_token"]
		if ok && token != "" {
			// 使用令牌的HTTPS URL
			cloneURL = fmt.Sprintf("https://%s@github.com/%s/%s.git", token, owner, repo)
		} else {
			// 公共HTTPS URL
			cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
		}
	case "gitlab":
		token, ok := credentials["gitlab_token"]
		if ok && token != "" {
			// 使用令牌的HTTPS URL
			cloneURL = fmt.Sprintf("https://oauth2:%s@gitlab.com/%s/%s.git", token, owner, repo)
		} else {
			// 公共HTTPS URL
			cloneURL = fmt.Sprintf("https://gitlab.com/%s/%s.git", owner, repo)
		}
	case "gitea":
		token, ok := credentials["gitea_token"]
		baseURL, baseOK := credentials["gitea_base_url"]
		
		if ok && baseOK && token != "" && baseURL != "" {
			// 移除尾部斜杠
			baseURL = strings.TrimSuffix(baseURL, "/")
			// 使用令牌的HTTPS URL - 在URL中包含身份验证
			cloneURL = fmt.Sprintf("https://%s@%s/%s/%s.git", 
				token,
				strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://"),
				owner, 
				repo)
		} else if baseOK && baseURL != "" {
			// 公共HTTPS URL
			cloneURL = fmt.Sprintf("%s/%s/%s.git", baseURL, owner, repo)
		} else {
			return "", fmt.Errorf("gitea_base_url is required for Gitea repositories")
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", platform)
	}
	
	return cloneURL, nil
}
