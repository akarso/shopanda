// Package integrationsdk provides stdlib-first HTTP and GraphQL clients for outbound
// integration plugins (warehouse APIs, PIM GraphQL, ERP REST).
//
// Subpackages:
//   - integrationsdk/http — REST client with timeouts and structured logging
//   - integrationsdk/graphql — thin GraphQL POST client built on integrationsdk/http
package integrationsdk

// Logger is the minimal logging surface used by integration clients.
// Pass nil to disable request logging.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}
