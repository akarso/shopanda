package promodemo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
)

const (
	RuleMinLineTotal     = "min_line_total"
	RuleLineBonusPercent = "line_bonus_percent"
)

type minLineTotalConfig struct {
	Type  string `json:"type"`
	Value int64  `json:"value"`
}

type lineBonusPercentConfig struct {
	Type       string `json:"type"`
	Percentage int    `json:"percentage"`
}

func evalMinLineTotal(_ context.Context, config []byte, item *extapi.PromotionPricingItem) (bool, error) {
	var cfg minLineTotalConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return false, fmt.Errorf("promodemo min_line_total: decode: %w", err)
	}
	if cfg.Value <= 0 {
		return false, fmt.Errorf("promodemo min_line_total: value must be positive")
	}
	if item == nil {
		return false, nil
	}
	return item.TotalAmount >= cfg.Value, nil
}

func evalLineBonusPercent(_ context.Context, config []byte, item *extapi.PromotionPricingItem) (int64, error) {
	var cfg lineBonusPercentConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 0, fmt.Errorf("promodemo line_bonus_percent: decode: %w", err)
	}
	if cfg.Percentage <= 0 || cfg.Percentage > 100 {
		return 0, fmt.Errorf("promodemo line_bonus_percent: percentage must be 1-100")
	}
	if item == nil || item.TotalAmount <= 0 {
		return 0, nil
	}
	return item.TotalAmount * int64(cfg.Percentage) / 100, nil
}
