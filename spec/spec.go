package spec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/imdario/mergo"
)

//
// Public values
//

// A set of constants for the different types of possible OpenAPI parameters.
const (
	ParameterPath  = "path"
	ParameterQuery = "query"
)

// A set of constant for the named types available in JSON Schema.
const (
	TypeArray   = "array"
	TypeBoolean = "boolean"
	TypeInteger = "integer"
	TypeNumber  = "number"
	TypeObject  = "object"
	TypeString  = "string"
)

//
// Public types
//

// Components is a struct for the components section of an OpenAPI
// specification.
type Components struct {
	Schemas    map[string]*Schema    `json:"schemas"`
	Parameters map[string]*Parameter `json:"parameters"`
	Responses  map[string]*Response  `json:"responses"`
}

// ExpansionResources is a struct for possible expansions in a resource.
type ExpansionResources struct {
	OneOf []*Schema `json:"oneOf"`
}

// Fixtures is a struct for a set of companion fixtures for an OpenAPI
// specification.
type Fixtures struct {
	Resources map[ResourceID]interface{} `json:"resources"`
}

// HTTPVerb is a type for an HTTP verb like GET, POST, etc.
type HTTPVerb string

// This is a list of fields that either we handle properly or we're confident
// it's safe to ignore. If a field not in this list appears in the OpenAPI spec,
// then we'll get an error so we remember to update telnyx-mock to support it.
var supportedSchemaFields = []string{
	"$ref",
	"additionalProperties",
	"allOf",
	"anyOf",
	"oneOf",
	"description",
	"discriminator",
	"enum",
	"example",
	"format",
	"items",
	"maxLength",
	"minLength",
	"maximum",
	"minimum",
	"default",
	"nullable",
	"pattern",
	"properties",
	"required",
	"title",
	"type",
	"readOnly",
	"writeOnly",
	"x-expandableFields",
	"x-expansionResources",
	"x-resourceId",
	"x-enum-descriptions",
	"x-enum-varnames",

	// This is currently being used to store additional metadata for our SDKs. It's
	// passed through our Spec and should be ignored
	"x-stripeResource",

	// This is currently a hint for the server-side so I haven't included it in
	// Schema yet. If we do start validating responses that come out of
	// stripe-mock, we may need to observe this as well.
	"x-stripeBypassValidation",
}

// Schema is a struct representing a JSON schema.
type Schema struct {
	// AdditionalProperties is either a `false` to indicate that no additional
	// properties in the object are allowed (beyond what's in Properties), or a
	// JSON schema that describes the expected format of any additional properties.
	//
	// We currently just read it as an `interface{}` because we're not using it
	// for anything right now.
	AdditionalProperties interface{} `json:"additionalProperties,omitempty"`

	// Discriminator is used for polymorphic responses, helping the client to
	// detect the object type
	//
	// We currently just read it as an `interface{}` because we're not using it
	Discriminator        interface{} `json:"discriminator,omitempty"`

	AllOf      []*Schema          `json:"allOf,omitempty"`
	AnyOf      []*Schema          `json:"anyOf,omitempty"`
	OneOf      []*Schema          `json:"oneOf,omitempty"`
	Enum       []interface{}      `json:"enum,omitempty"`
	Format     string             `json:"format,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
	MaxLength  int                `json:"maxLength,omitempty"`
	MinLength  int                `json:"minLength,omitempty"`
	Minimum    int                `json:"minimum,omitempty"`
	Maximum    int                `json:"maximum,omitempty"`
	Default    json.RawMessage    `json:"default,omitempty"`
	Nullable   bool               `json:"nullable,omitempty"`
	Example    json.RawMessage    `json:"example,omitempty"`
	Pattern    string             `json:"pattern,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
	Type       string             `json:"type,omitempty"`
	WriteOnly  bool               `json:"writeOnly,omitempty"`
	ReadOnly   bool               `json:"readOnly,omitempty"`

	// Ref is populated if this JSON Schema is actually a JSON reference, and
	// it defines the location of the actual schema definition.
	Ref string `json:"$ref,omitempty"`

	XExpandableFields   *[]string           `json:"x-expandableFields,omitempty"`
	XExpansionResources *ExpansionResources `json:"x-expansionResources,omitempty"`
	XResourceID         string              `json:"x-resourceId,omitempty"`
}

func (s *Schema) String() string {
	js, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(js)
}

// UnmarshalJSON is a custom JSON unmarshaling implementation for Schema that
// provides better error messages instead of silently ignoring fields.
func (s *Schema) UnmarshalJSON(data []byte) error {
	var rawFields map[string]interface{}
	err := json.Unmarshal(data, &rawFields)
	if err != nil {
		return err
	}

	for _, supportedField := range supportedSchemaFields {
		delete(rawFields, supportedField)
	}
	for unsupportedField := range rawFields {
		return fmt.Errorf(
			"unsupported field in JSON schema: '%s'", unsupportedField)
	}

	// Define a second type that's identical to Schema, but distinct, so that when
	// we call json.Unmarshal it will call the default implementation of
	// unmarshalling a Schema object instead of recursively calling this
	// UnmarshalJSON function again.
	type schemaAlias Schema
	var inner schemaAlias
	err = json.Unmarshal(data, &inner)
	if err != nil {
		return err
	}
	*s = Schema(inner)

	return nil
}

// FlattenAllOf will flatten the AllOf []*Schema slice and return a new
// single *Schema
func (s *Schema) FlattenAllOf() *Schema {
	var flatten func(output *Schema, input *Schema)

	flatten = func(output *Schema, input *Schema) {
		allOf := input.AllOf

		// Nillify `AllOf` so `mergo` will skip it in the merge. We don't want
		// the `AllfOf` slice being added to the output.
		input.AllOf = nil

		mergo.Merge(output, input)

		// Now add it back so we don't cause side affects
		input.AllOf = allOf

		for _, v := range allOf {
			flatten(output, v)
		}
	}

	var output Schema

	flatten(&output, s)

	return &output
}

// ResolveRef returns the ultimate *Schema.
//
// If Ref is nil, the same *Schema is returned that was passed in. Otherwise,
// the *Schema will be resolved from the provided schemas map.
func (s *Schema) ResolveRef(schemas map[string]*Schema) (*Schema, error) {
	if s.Ref == "" {
		return s, nil
	}

	schemaName := strings.SplitAfterN(s.Ref, "#/components/schemas/", 2)
	schema, ok := schemas[schemaName[1]]

	if !ok {
		return nil, fmt.Errorf("Could not find response %s in #/components/responses/", schemaName)
	}

	return schema, nil
}

// MediaType is a struct bucketing a request or response by media type in an
// OpenAPI specification.
type MediaType struct {
	Schema *Schema `json:"schema"`
}

// Operation is a struct representing a possible HTTP operation in an OpenAPI
// specification.
type Operation struct {
	Description string                  `json:"description"`
	OperationID string                  `json:"operation_id"`
	Parameters  []*Parameter            `json:"parameters"`
	RequestBody *RequestBody            `json:"requestBody"`
	Responses   map[StatusCode]Response `json:"responses"`
}

// Parameter is a struct representing a request parameter to an HTTP operation
// in an OpenAPI specification.
type Parameter struct {
	Description string  `json:"description"`
	In          string  `json:"in"`
	Name        string  `json:"name"`
	Required    bool    `json:"required"`
	Schema      *Schema `json:"schema"`
	Ref         string  `json:"$ref,omitempty"`
}

// Path is a type for an HTTP path in an OpenAPI specification.
type Path string

// RequestBody is a struct representing the body of a request in an OpenAPI
// specification.
type RequestBody struct {
	Content  map[string]MediaType `json:"content"`
	Required bool                 `json:"required"`
}

// Response is a struct representing the response of an HTTP operation in an
// OpenAPI specification.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content"`
	// Ref is populated if this JSON Schema is actually a JSON reference, and
	// it defines the location of the actual schema definition.
	Ref string `json:"$ref,omitempty"`
}

// ResolveRef returns the ultimate *Response.
//
// If Ref is nil, the same *Response is returned that was passed in. Otherwise,
// the *Response will be resolved from the provided responses map.
func (r *Response) ResolveRef(responses map[string]*Response) (*Response, error) {
	if r.Ref == "" {
		return r, nil
	}

	responseName := strings.SplitAfterN(r.Ref, "#/components/responses/", 2)
	responseObject, ok := responses[responseName[1]]

	if !ok {
		return nil, fmt.Errorf("Could not find response %s in #/components/responses/", responseName)
	}

	return responseObject, nil
}

// ResourceID is a type for the ID of a response resource in an OpenAPI
// specification.
type ResourceID string

// Spec is a struct representing an OpenAPI specification.
type Spec struct {
	Components Components                       `json:"components"`
	Paths      map[Path]map[HTTPVerb]*Operation `json:"paths"`
}

// Flatten will walk the Paths and flatten the RequestBody AllOf slices to
// a single Schema.
func (s *Spec) Flatten() {
	for _, verbs := range s.Paths {
		for _, operation := range verbs {
			if operation.RequestBody == nil {
				continue
			}

			var contentType string
			var mediaType MediaType

			for c, m := range operation.RequestBody.Content {
				contentType = c
				mediaType = m

				break
			}

			schema := mediaType.Schema

			newSchema := schema.FlattenAllOf()

			operation.RequestBody.Content[contentType] = MediaType{Schema: newSchema}
		}
	}
}

// StatusCode is a type for the response status code of an HTTP operation in an
// OpenAPI specification.
type StatusCode string
