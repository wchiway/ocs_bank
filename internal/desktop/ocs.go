package desktop

import (
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed ocs.json
var ocsTemplate []byte

type OCSExport struct {
	URL  string `json:"url"`
	JSON string `json:"json"`
}

func (c *Controller) ExportOCS() (OCSExport, error) {
	c.op.Lock()
	defer c.op.Unlock()
	cfg, exists, err := c.readDocument()
	if err != nil {
		return OCSExport{}, err
	}
	if !exists {
		return OCSExport{}, errors.New("请先保存模型设置，再导出 OCS 配置")
	}
	cfg.APIKey = "unused-export-placeholder"
	if err = c.validate(&cfg); err != nil {
		return OCSExport{}, err
	}
	var wrappers []map[string]any
	if json.Unmarshal(ocsTemplate, &wrappers) != nil || len(wrappers) != 1 {
		return OCSExport{}, errors.New("无法生成 OCS 配置")
	}
	url := queryURL(portOf(cfg.Listen))
	wrappers[0]["url"] = url
	body, err := json.MarshalIndent(wrappers, "", "  ")
	if err != nil {
		return OCSExport{}, errors.New("无法生成 OCS 配置")
	}
	return OCSExport{URL: url, JSON: string(body)}, nil
}
