package returns

// Domain event names for return workflow observability.
const (
	EventReturnRequested = "return.requested"
	EventReturnApproved  = "return.approved"
	EventReturnRejected  = "return.rejected"
	EventReturnCancelled = "return.cancelled"
	EventReturnReceived  = "return.received"
	EventReturnRefunded  = "return.refunded"
)

// ReturnEventData is the payload for return lifecycle events.
type ReturnEventData struct {
	ReturnID   string
	OrderID    string
	Status     Status
	CustomerID string
}
