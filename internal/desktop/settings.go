package desktop

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/wchiway/ocs_bank/internal/config"
)

func (c *Controller) SaveSettings(request SaveRequest) (Settings, error) {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	blocked := c.closed || c.state != "stopped"
	c.mu.Unlock()
	if blocked {
		return Settings{}, errors.New("请先停止服务，再保存设置")
	}
	secrets, err := c.readSecrets()
	if err != nil {
		return Settings{}, err
	}
	key := strings.TrimSpace(request.APIKey)
	if key != "" {
		secrets["AI_API_KEY"] = key
	}
	if strings.TrimSpace(secrets["AI_API_KEY"]) == "" {
		return Settings{}, errors.New("首次设置需要填写 API Key")
	}
	cfg := config.Config{
		Listen: net.JoinHostPort("127.0.0.1", strconv.Itoa(request.Port)), Database: filepath.Join(c.root, "question_bank.db"),
		Protocol: request.Protocol, BaseURL: strings.TrimSpace(request.BaseURL), Model: strings.TrimSpace(request.Model), EndpointPath: strings.TrimSpace(request.EndpointPath),
		ReasoningEffort: request.ReasoningEffort, ChatTokenParameter: request.ChatTokenParameter, TimeoutSeconds: request.TimeoutSeconds,
		MaxConcurrent: request.MaxConcurrent, MaxPending: request.MaxPending, MaxRetries: request.MaxRetries, MaxTokens: request.MaxTokens,
		RequestsPerMinute: request.RequestsPerMinute, CacheTTLHours: request.CacheTTLHours, APIKey: secrets["AI_API_KEY"],
	}
	if err = c.validate(&cfg); err != nil {
		return Settings{}, err
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Settings{}, errors.New("无法生成设置文件")
	}
	secretBody, err := godotenv.Marshal(secrets)
	if err != nil {
		return Settings{}, errors.New("无法保存 API Key")
	}
	if err = os.MkdirAll(c.root, 0700); err != nil {
		return Settings{}, errors.New("无法创建数据目录，请检查目录权限")
	}
	oldSecret, secretExists, err := readOptional(filepath.Join(c.root, ".env"))
	if err != nil {
		return Settings{}, errors.New("无法读取 API Key 设置")
	}
	secretTemp, err := stageFile(c.root, []byte(secretBody+"\n"))
	if err != nil {
		return Settings{}, errors.New("无法写入 API Key 设置，请检查目录权限和磁盘空间")
	}
	defer os.Remove(secretTemp)
	configTemp, err := stageFile(c.root, append(body, '\n'))
	if err != nil {
		return Settings{}, errors.New("无法写入设置，请检查目录权限和磁盘空间")
	}
	defer os.Remove(configTemp)
	wasPending, err := c.savePending()
	if err != nil {
		return Settings{}, err
	}
	marker := filepath.Join(c.root, ".settings-pending")
	if err = os.WriteFile(marker, nil, 0600); err != nil {
		return Settings{}, errors.New("无法开始保存设置，请检查目录权限")
	}
	// Two files cannot be renamed atomically together. A marker prevents startup
	// after an interrupted save; a failed configuration rename restores the key.
	if err = os.Rename(secretTemp, filepath.Join(c.root, ".env")); err != nil {
		if !wasPending {
			_ = os.Remove(marker)
		}
		return Settings{}, errors.New("无法替换 API Key 设置，请关闭占用文件的程序后重试")
	}
	if err = os.Rename(configTemp, filepath.Join(c.root, "config.json")); err != nil {
		if c.restoreSecret(oldSecret, secretExists) == nil && !wasPending {
			_ = os.Remove(marker)
		}
		return Settings{}, errors.New("设置保存未完成，请关闭占用文件的程序后重新保存")
	}
	if err = os.Remove(marker); err != nil {
		return Settings{}, errors.New("设置已写入，但保存未完成，请重新保存设置")
	}
	s := c.settings(cfg)
	s.HasAPIKey, s.Configured = true, true
	c.mu.Lock()
	c.configured, c.port, c.lastError = true, s.Port, ""
	c.mu.Unlock()
	return s, nil
}

func (c *Controller) readSecrets() (map[string]string, error) {
	body, _, err := readOptional(filepath.Join(c.root, ".env"))
	if err != nil {
		return nil, errors.New("无法读取 API Key 设置，请检查数据目录权限")
	}
	values, err := godotenv.Unmarshal(string(body))
	if err != nil {
		return nil, errors.New("API Key 设置文件格式错误，请检查数据目录中的 .env")
	}
	return values, nil
}

func readOptional(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(body) > 65536 {
		return nil, true, errors.New("file too large or unreadable")
	}
	return body, true, nil
}

func stageFile(dir string, body []byte) (path string, err error) {
	f, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return "", err
	}
	path = f.Name()
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	return path, err
}

func (c *Controller) restoreSecret(body []byte, exists bool) error {
	path := filepath.Join(c.root, ".env")
	if !exists {
		return os.Remove(path)
	}
	temp, err := stageFile(c.root, body)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return os.Rename(temp, path)
}

func (c *Controller) savePending() (bool, error) {
	_, err := os.Stat(filepath.Join(c.root, ".settings-pending"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("无法确认设置保存状态，请检查数据目录权限")
	}
	return true, nil
}
