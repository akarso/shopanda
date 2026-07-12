package extapi

// HookPoint names a stable synchronous hook chain point.
type HookPoint string

// HookHandler mutates hook context during chain execution.
type HookHandler func(ctx *HookContext) error

// HookContext carries mutable hook payload across handlers in a chain.
type HookContext struct {
	Name    string
	Payload map[string]interface{}
}

// Get returns a payload value.
func (c *HookContext) Get(key string) (interface{}, bool) {
	if c == nil || c.Payload == nil {
		return nil, false
	}
	v, ok := c.Payload[key]
	return v, ok
}

// Set stores a payload value.
func (c *HookContext) Set(key string, value interface{}) {
	if c == nil {
		return
	}
	if c.Payload == nil {
		c.Payload = make(map[string]interface{})
	}
	c.Payload[key] = value
}

const (
	HookCartAddItemBefore      HookPoint = "cart.add_item.before"
	HookCartAddItemAfter       HookPoint = "cart.add_item.after"
	HookCartUpdateItemBefore   HookPoint = "cart.update_item.before"
	HookCartRemoveItemAfter    HookPoint = "cart.remove_item.after"
	HookCartRecalculateBefore  HookPoint = "cart.recalculate.before"
)

var hookPoints = []HookPoint{
	HookCartAddItemBefore,
	HookCartAddItemAfter,
	HookCartUpdateItemBefore,
	HookCartRemoveItemAfter,
	HookCartRecalculateBefore,
}

// HookPoints returns all documented stable hook point names.
func HookPoints() []string {
	out := make([]string, len(hookPoints))
	for i, point := range hookPoints {
		out[i] = string(point)
	}
	return out
}
