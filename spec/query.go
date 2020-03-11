package spec

import (
	"fmt"
	"strings"
)

// BuildQuerySchema builds a JSON schema that will be used to validate query
// parameters on the incoming request. Unlike request bodies, OpenAPI puts
// query parameters in a different, non-JSON schema part of an operation.
func BuildQuerySchema(operation *Operation, parameters map[string]*Parameter) (*Schema, error) {
	schema := &Schema{
		AdditionalProperties: false,
		Properties:           make(map[string]*Schema),
		Required:             make([]string, 0),
		Type:                 TypeObject,
	}

	if operation.Parameters == nil {
		return schema, nil
	}

	for _, param := range operation.Parameters {
		if param.Ref != "" {
			refParts := strings.SplitAfterN(param.Ref, "#/components/parameters/", 2)
			refName := refParts[1]

			if v, ok := parameters[refName]; ok {
				param = v
			} else {
				return nil, fmt.Errorf("invalid $ref '%s'", param.Ref)
			}
		}

		if param.In != ParameterQuery {
			continue
		}

		paramSchema := param.Schema
		if paramSchema == nil {
			paramSchema = &Schema{Type: TypeObject}
		}
		schema.Properties[param.Name] = paramSchema

		if param.Required {
			schema.Required = append(schema.Required, param.Name)
		}
	}

	return schema, nil
}
