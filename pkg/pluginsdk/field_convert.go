package pluginsdk

import (
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/pkg/extapi"
)

func fieldDefToDomain(def extapi.FieldDef) domainext.FieldDef {
	return domainext.FieldDef{
		Code:        def.Code,
		Label:       def.Label,
		Description: def.Description,
		Type:        domainext.FieldType(def.Type),
		Scope:       domainext.TargetType(def.Scope),
		StorageMode: domainext.StorageMode(def.StorageMode),
		Visibility:  domainext.Visibility(def.Visibility),
		Access: domainext.Access{
			ReadRoles:       append([]string(nil), def.Access.ReadRoles...),
			WriteRoles:      append([]string(nil), def.Access.WriteRoles...),
			PluginOnlyWrite: def.Access.PluginOnlyWrite,
			ExclusiveString: def.Access.ExclusiveString,
		},
		Validation: domainext.Validation{
			Required: def.Validation.Required,
			Min:      def.Validation.Min,
			Max:      def.Validation.Max,
			Regex:    def.Validation.Regex,
			Options:  append([]string(nil), def.Validation.Options...),
		},
	}
}
