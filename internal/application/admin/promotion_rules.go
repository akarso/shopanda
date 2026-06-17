package admin

import (
	"encoding/json"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/promotion"
)

// PromotionRuleForm is the admin-facing representation of promotion conditions/actions.
type PromotionRuleForm struct {
	ConditionType    string // always, min_quantity, min_cart_total
	ConditionValue   int
	ActionType       string // percentage, fixed
	ActionPercentage int
	ActionAmount     int64 // minor currency units
}

// EncodePromotionRules maps admin form fields to JSON stored on promotion.Promotion.
func EncodePromotionRules(typ promotion.PromotionType, form PromotionRuleForm) (conditions, actions []byte, err error) {
	conditions, err = encodePromotionCondition(typ, form)
	if err != nil {
		return nil, nil, err
	}
	actions, err = encodePromotionAction(form)
	if err != nil {
		return nil, nil, err
	}
	return conditions, actions, nil
}

// DecodePromotionRules maps stored JSON back to admin form fields.
func DecodePromotionRules(typ promotion.PromotionType, conditions, actions []byte) (PromotionRuleForm, error) {
	form, err := decodePromotionCondition(typ, conditions)
	if err != nil {
		return PromotionRuleForm{}, err
	}
	actionForm, err := decodePromotionAction(actions)
	if err != nil {
		return PromotionRuleForm{}, err
	}
	form.ActionType = actionForm.ActionType
	form.ActionPercentage = actionForm.ActionPercentage
	form.ActionAmount = actionForm.ActionAmount
	return form, nil
}

type conditionPayload struct {
	Type  string `json:"type"`
	Value int    `json:"value,omitempty"`
}

func encodePromotionCondition(typ promotion.PromotionType, form PromotionRuleForm) ([]byte, error) {
	condType := form.ConditionType
	if condType == "" {
		condType = "always"
	}
	switch condType {
	case "always":
		return json.Marshal(conditionPayload{Type: "always"})
	case "min_quantity":
		if typ != promotion.TypeCatalog {
			return nil, fmt.Errorf("min_quantity condition applies only to catalog promotions")
		}
		if form.ConditionValue <= 0 {
			return nil, fmt.Errorf("min_quantity value must be positive")
		}
		return json.Marshal(conditionPayload{Type: "min_quantity", Value: form.ConditionValue})
	case "min_cart_total":
		if typ != promotion.TypeCart {
			return nil, fmt.Errorf("min_cart_total condition applies only to cart promotions")
		}
		if form.ConditionValue <= 0 {
			return nil, fmt.Errorf("min_cart_total value must be positive")
		}
		return json.Marshal(conditionPayload{Type: "min_cart_total", Value: form.ConditionValue})
	default:
		return nil, fmt.Errorf("unknown condition type: %q", condType)
	}
}

func decodePromotionCondition(typ promotion.PromotionType, data []byte) (PromotionRuleForm, error) {
	if len(data) == 0 || string(data) == "null" || string(data) == "[]" {
		return PromotionRuleForm{ConditionType: "always"}, nil
	}
	var cfg conditionPayload
	if err := json.Unmarshal(data, &cfg); err != nil {
		return PromotionRuleForm{}, fmt.Errorf("decode conditions: %w", err)
	}
	switch cfg.Type {
	case "", "always":
		return PromotionRuleForm{ConditionType: "always"}, nil
	case "min_quantity":
		if typ != promotion.TypeCatalog {
			return PromotionRuleForm{}, fmt.Errorf("min_quantity condition is invalid for type %q", typ)
		}
		return PromotionRuleForm{ConditionType: "min_quantity", ConditionValue: cfg.Value}, nil
	case "min_cart_total":
		if typ != promotion.TypeCart {
			return PromotionRuleForm{}, fmt.Errorf("min_cart_total condition is invalid for type %q", typ)
		}
		return PromotionRuleForm{ConditionType: "min_cart_total", ConditionValue: cfg.Value}, nil
	default:
		return PromotionRuleForm{}, fmt.Errorf("unsupported condition type: %q", cfg.Type)
	}
}

type actionPayload struct {
	Type       string `json:"type"`
	Percentage int    `json:"percentage,omitempty"`
	Amount     int64  `json:"amount,omitempty"`
}

func encodePromotionAction(form PromotionRuleForm) ([]byte, error) {
	switch form.ActionType {
	case "percentage":
		if form.ActionPercentage <= 0 || form.ActionPercentage > 100 {
			return nil, fmt.Errorf("percentage must be between 1 and 100")
		}
		return json.Marshal(actionPayload{Type: "percentage", Percentage: form.ActionPercentage})
	case "fixed":
		if form.ActionAmount <= 0 {
			return nil, fmt.Errorf("fixed amount must be positive")
		}
		return json.Marshal(actionPayload{Type: "fixed", Amount: form.ActionAmount})
	default:
		return nil, fmt.Errorf("unknown action type: %q", form.ActionType)
	}
}

func decodePromotionAction(data []byte) (PromotionRuleForm, error) {
	if len(data) == 0 || string(data) == "null" || string(data) == "[]" {
		return PromotionRuleForm{}, fmt.Errorf("action config is required")
	}
	var cfg actionPayload
	if err := json.Unmarshal(data, &cfg); err != nil {
		return PromotionRuleForm{}, fmt.Errorf("decode actions: %w", err)
	}
	switch cfg.Type {
	case "percentage":
		return PromotionRuleForm{
			ActionType:       "percentage",
			ActionPercentage: cfg.Percentage,
		}, nil
	case "fixed":
		return PromotionRuleForm{
			ActionType:   "fixed",
			ActionAmount: cfg.Amount,
		}, nil
	default:
		return PromotionRuleForm{}, fmt.Errorf("unsupported action type: %q", cfg.Type)
	}
}
