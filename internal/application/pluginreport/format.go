package pluginreport

import (
	"encoding/json"
	"fmt"
	"strings"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	importctxapp "github.com/akarso/shopanda/internal/application/importctx"
)

// FormatText renders report as human-readable plain text.
func FormatText(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plugin registration report (generated %s)\n\n", report.GeneratedAt.Format(timeRFC3339))

	b.WriteString("Plugins:\n")
	if len(report.Plugins) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, p := range report.Plugins {
			line := fmt.Sprintf("  %-32s %s", p.Name, p.State)
			if p.Error != "" {
				line += " — " + p.Error
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	b.WriteString("\nInfrastructure ports:\n")
	if len(report.Ports) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, port := range report.Ports {
			impl := port.Implementation
			if impl == "" && len(port.Providers) > 0 {
				impl = port.Providers[0].Implementation
			}
			fmt.Fprintf(&b, "  %-10s %-16s %s", port.Name, port.Status, impl)
			if port.Driver != "" {
				fmt.Fprintf(&b, " (%s)", port.Driver)
			}
			b.WriteByte('\n')
		}
	}

	b.WriteString("\nCore pricing steps:\n  ")
	b.WriteString(strings.Join(report.CorePricingSteps, " → "))
	b.WriteByte('\n')

	b.WriteString("\nPlugin pricing steps:\n")
	if len(report.PricingSteps) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, step := range report.PricingSteps {
			fmt.Fprintf(&b, "  %-18s %-20s %s\n", step.Position, step.Name, step.Type)
		}
	}

	writeHooksSection(&b, report.Hooks)
	writeImportHooksSection(&b, report.ImportHooks)

	b.WriteString("\nComposition steps:\n")
	if len(report.CompositionSteps) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, step := range report.CompositionSteps {
			fmt.Fprintf(&b, "  %-6s %-24s %s\n", step.Pipeline, step.Name, step.Type)
		}
	}

	b.WriteString("\nCheckout steps:\n")
	if len(report.CheckoutSteps) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, step := range report.CheckoutSteps {
			fmt.Fprintf(&b, "  %-24s %s\n", step.Name, step.Type)
		}
	}

	b.WriteString("\nSync jobs:\n")
	if len(report.SyncJobs) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, job := range report.SyncJobs {
			detail := job.Detail
			if detail == "" {
				detail = job.Trigger
			}
			fmt.Fprintf(&b, "  %s  %s  %s\n", job.JobType, detail, job.PluginSlug)
		}
	}

	b.WriteString("\nPublic routes:\n")
	if len(report.PublicRoutes) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, route := range report.PublicRoutes {
			fmt.Fprintf(&b, "  %s\n", route.Pattern)
		}
	}

	b.WriteString("\nAdmin routes:\n")
	if len(report.AdminRoutes) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, route := range report.AdminRoutes {
			fmt.Fprintf(&b, "  %s  [%s]\n", route.Pattern, route.Permission)
		}
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

func writeHooksSection(b *strings.Builder, hooks []hooksapp.CatalogEntry) {
	b.WriteString("\nHooks:\n")
	if len(hooks) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	for _, entry := range hooks {
		for _, handler := range entry.Handlers {
			fmt.Fprintf(b, "  %-28s priority=%-4d %s\n", entry.Name, handler.Priority, handler.Registrant)
		}
	}
}

func writeImportHooksSection(b *strings.Builder, hooks []importctxapp.CatalogEntry) {
	b.WriteString("\nImport row hooks:\n")
	if len(hooks) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	for _, entry := range hooks {
		for _, handler := range entry.Handlers {
			fmt.Fprintf(b, "  %-12s priority=%-4d %s\n", entry.Entity, handler.Priority, handler.Registrant)
		}
	}
}

// FormatJSON renders report as indented JSON.
func FormatJSON(report Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
