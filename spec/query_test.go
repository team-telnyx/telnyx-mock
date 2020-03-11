package spec

import (
	"testing"

	assert "github.com/stretchr/testify/require"
)

func TestBuildQuerySchema(t *testing.T) {
	// Handles a normal case
	{
		operation := &Operation{
			Parameters: []*Parameter{
				{
					In:   ParameterQuery,
					Name: "name",
					Schema: &Schema{
						Type: TypeString,
					},
				},
			},
		}
		schema, _ := BuildQuerySchema(operation, map[string]*Parameter{})

		assert.Equal(t, false, schema.AdditionalProperties)
		assert.Equal(t, 1, len(schema.Properties))
		assert.Equal(t, 0, len(schema.Required))

		paramSchema := schema.Properties["name"]
		assert.Equal(t, TypeString, paramSchema.Type)
	}

	// A non-query parameter
	{
		operation := &Operation{
			Parameters: []*Parameter{
				{
					In:   ParameterPath,
					Name: "name",
				},
			},
		}
		schema, _ := BuildQuerySchema(operation, map[string]*Parameter{})

		assert.Equal(t, 0, len(schema.Properties))
	}

	// A required parameter
	{
		operation := &Operation{
			Parameters: []*Parameter{
				{
					In:       ParameterQuery,
					Name:     "name",
					Required: true,
					Schema: &Schema{
						Type: TypeString,
					},
				},
			},
		}
		schema, _ := BuildQuerySchema(operation, map[string]*Parameter{})

		assert.Equal(t, []string{"name"}, schema.Required)
	}

	// A query parameter with no schema
	{
		operation := &Operation{
			Parameters: []*Parameter{
				{
					In:   ParameterQuery,
					Name: "name",
				},
			},
		}
		schema, _ := BuildQuerySchema(operation, map[string]*Parameter{})

		paramSchema := schema.Properties["name"]
		assert.Equal(t, TypeObject, paramSchema.Type)
	}

	// A '$ref' parameter
	{
		operation := &Operation{
			Parameters: []*Parameter{
				{
					Ref: "#/components/parameters/PageNum",
				},
			},
		}

		parameters := map[string]*Parameter{
			"PageNum": {
				In:   ParameterQuery,
				Name: "name",
				Schema: &Schema{
					Type: TypeString,
				},
			},
		}

		schema, _ := BuildQuerySchema(operation, parameters)

		assert.Equal(t, false, schema.AdditionalProperties)
		assert.Equal(t, 1, len(schema.Properties))
		assert.Equal(t, 0, len(schema.Required))

		paramSchema := schema.Properties["name"]
		assert.Equal(t, TypeString, paramSchema.Type)
	}

	// An error is returned when an invalid `$ref` is supplied
	{
		operation := &Operation{
			Parameters: []*Parameter{
				{
					Ref: "#/components/parameters/PageNum",
				},
			},
		}

		_, err := BuildQuerySchema(operation, map[string]*Parameter{})

		assert.NotNil(t, err)
	}
}
