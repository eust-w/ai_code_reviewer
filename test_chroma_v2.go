package main

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"
)

func main() {
	// 设置日志级别
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// 直接使用 curl 测试 Chroma API
	logrus.Infof("Testing Chroma API v2 endpoints...")

	// 测试 heartbeat 端点
	cmd := "curl -v \"http://localhost:8012/api/v2/heartbeat\""
	logrus.Infof("Running: %s", cmd)
	output, err := execCommand(cmd)
	if err != nil {
		logrus.Errorf("Error: %v", err)
	} else {
		logrus.Infof("Result: %s", output)
	}

	// 测试 tenants 端点
	cmd = "curl -v \"http://localhost:8012/api/v2/tenants\""
	logrus.Infof("Running: %s", cmd)
	output, err = execCommand(cmd)
	if err != nil {
		logrus.Errorf("Error: %v", err)
	} else {
		logrus.Infof("Result: %s", output)
	}

	// 测试 databases 端点
	cmd = "curl -v \"http://localhost:8012/api/v2/tenants/default/databases\""
	logrus.Infof("Running: %s", cmd)
	output, err = execCommand(cmd)
	if err != nil {
		logrus.Errorf("Error: %v", err)
	} else {
		logrus.Infof("Result: %s", output)
	}

	// 测试创建数据库
	cmd = "curl -v -X POST -H \"Content-Type: application/json\" -d '{\"name\":\"default\"}' \"http://localhost:8012/api/v2/tenants/default/databases\""
	logrus.Infof("Running: %s", cmd)
	output, err = execCommand(cmd)
	if err != nil {
		logrus.Errorf("Error: %v", err)
	} else {
		logrus.Infof("Result: %s", output)
	}

	// 测试 collections 端点
	cmd = "curl -v \"http://localhost:8012/api/v2/tenants/default/databases/default/collections\""
	logrus.Infof("Running: %s", cmd)
	output, err = execCommand(cmd)
	if err != nil {
		logrus.Errorf("Error: %v", err)
	} else {
		logrus.Infof("Result: %s", output)
	}

	// 测试创建集合
	cmd = "curl -v -X POST -H \"Content-Type: application/json\" -d '{\"name\":\"test_collection\",\"get_or_create\":true}' \"http://localhost:8012/api/v2/tenants/default/databases/default/collections\""
	logrus.Infof("Running: %s", cmd)
	output, err = execCommand(cmd)
	if err != nil {
		logrus.Errorf("Error: %v", err)
	} else {
		logrus.Infof("Result: %s", output)
	}
}

// execCommand 执行命令并返回输出
func execCommand(cmd string) (string, error) {
	// 使用 os/exec 包执行命令
	command := exec.Command("bash", "-c", cmd)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command execution failed: %w, output: %s", err, string(output))
	}
	
	// 添加短暂延迟，避免请求过快
	time.Sleep(500 * time.Millisecond)
	
	// 返回命令输出
	return string(output), nil
}
